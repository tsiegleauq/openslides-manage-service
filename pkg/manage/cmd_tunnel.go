package manage

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"

	"github.com/OpenSlides/openslides-manage-service/proto"
	"github.com/spf13/cobra"
	"google.golang.org/grpc/metadata"
)

const helpTunnel = `Opens local ports to all services and creates
tunnels into the OpenSlides network to the services they belong to.

Without any argument, the command creates tunnels to all services at ther 
default ports. To specify tunnels, the flag "-L" can be used or the name
of a known service as argument.

The syntax of the -L argument is the same as "ssh -L". The argument can be
used more then one time.

Example:

Open tunnels to all known services:
$ manage tunnel

Open tunnels to the datastore and auth
$ manage tunnel datastore-reader datastore-writer auth

Open a tunnel to auth on localhost:8080
$ manage tunnel -L localhost:8080:auth:9004
 `

func cmdTunnel(cfg *ClientConfig) *cobra.Command {
	services := map[string]string{
		"message-bus":       ":6379:message-bus:6379",
		"datastore-reader":  ":9010:datastore-reader:9010",
		"datastore-writer":  ":9011:datastore-writer:9011",
		"backend-action":    ":9002:backend:9002",
		"backend-presenter": ":9003:backend:9003",
		"autoupdate":        ":9012:autoupdate:9012",
		"permission":        ":9005:permission:9005", // TODO: Remove after permission is removed.
		"auth":              ":9004:auth:9004",
		"media":             ":9006:media:9006",
		"postgres":          ":5432:postgres:5432",
		"cache":             ":6379:cache:6379", // TODO: Another port would be nice.
		// TODO: Add voting after it was added.
	}

	var serviceNames []string
	for service := range services {
		serviceNames = append(serviceNames, service)
	}

	cmd := cobra.Command{
		Use:       "tunnel",
		Short:     "Creates tcp tunnels to the OpenSlides network.",
		Long:      helpTunnel,
		Args:      cobra.OnlyValidArgs,
		ValidArgs: serviceNames,
	}

	addrsF := cmd.Flags().StringArrayP("addr", "L", nil, "[bind_address:]port:host:hostport")

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
		defer cancel()

		service, close, err := Dial(ctx, cfg.Address)
		if err != nil {
			return fmt.Errorf("connecting to gRPC server: %w", err)
		}
		defer close()

		if len(*addrsF) == 0 && len(args) == 0 {
			// No tunnel was specified. Use all services.

			// Remove cache from all serviceName. It has the same port as the
			// messageBus. TODO: Use another port for the cache.
			delete(services, "cache")

			args = serviceNames
		}
		for _, arg := range args {
			a, ok := services[arg]
			if !ok {
				// This is only necessary to remove the cache if all services
				// are used. Remove this after the cache has its own port.
				continue
			}
			*addrsF = append(*addrsF, a)
		}
		addrs := tunnelParseArgument(*addrsF)

		var wg sync.WaitGroup
		for local, remote := range addrs {
			wg.Add(1)
			go func(local, remote string) {
				defer wg.Done()
				if err := newTunnel(service, local, remote); err != nil {
					log.Printf("Error connecting %s to %s: %v", local, remote, err)
					return
				}
			}(local, remote)
		}

		wg.Wait()

		return nil
	}
	return &cmd
}

// tunnelParseArgument parses the "-L" argument of the tunnel command.
//
// The argument is a list of the values of all the "-L" arguments.
//
// The keys of the returned map are the local addr (for example ":9002") and the
// values the remote addr (for example "backend:9002").
func tunnelParseArgument(args []string) map[string]string {
	m := make(map[string]string, len(args))
	for _, arg := range args {
		parts := strings.Split(arg, ":")
		if len(parts) == 3 {
			parts = append([]string{""}, parts...)
		}
		m[parts[0]+":"+parts[1]] = parts[2] + ":" + parts[3]
	}
	return m
}

// newTunnel creates a new tunnel via grpc to the manage service.
//
// Listens on the given localAddr, sends all data via grpc to the manage server
// and there redirect it to the remoteAddr.
//
// Blocks until the tunnel is closed.
func newTunnel(service proto.ManageClient, localAddr string, remoteAddr string) error {
	// Listen on localAddr
	lst, err := net.Listen("tcp", localAddr)
	if err != nil {
		return fmt.Errorf("start listening on %s: %w", localAddr, err)
	}
	defer lst.Close()
	log.Printf("Listen on %s", localAddr)

	// Waiting for connections
	for {
		conn, err := lst.Accept()
		if err != nil {
			log.Printf("Error accepting connection: %v", err)
			continue
		}

		go func(conn net.Conn) {
			defer conn.Close()

			// Open Tunnel
			ctx := metadata.NewOutgoingContext(
				context.Background(),
				metadata.Pairs("addr", remoteAddr),
			)
			tunnel, err := service.Tunnel(ctx)
			if err != nil {
				log.Printf("Error creating tunnel: %v", err)
				return
			}

			// Connecting the local connection to the tunnel
			if err := copyStream(tunnel, conn); err != nil {
				log.Printf("Error tunneling data: %v", err)
				return
			}
		}(conn)
	}
}

// Tunnel redirects a package to a different service.
func (s *Server) Tunnel(ts proto.Manage_TunnelServer) error {
	md, ok := metadata.FromIncomingContext(ts.Context())
	if !ok {
		return fmt.Errorf("unable to get metadata from context")
	}
	addr := md.Get("addr")
	if len(addr) != 1 {
		return fmt.Errorf("expect one address (host:port) in the meta data")
	}

	conn, err := new(net.Dialer).DialContext(ts.Context(), "tcp", addr[0])
	if err != nil {
		return fmt.Errorf("connecting to %s: %w", addr[0], err)
	}
	defer conn.Close()

	if err := copyStream(ts, conn); err != nil {
		return fmt.Errorf("connection grpc to server: %w", err)
	}

	return nil
}

// sendReceiver reads and writes from a grpc tunnel connection.
type sendReceiver interface {
	receiver
	sender
}

type receiver interface {
	Recv() (*proto.TunnelData, error)
}

type sender interface {
	Send(*proto.TunnelData) error
}

// copyStream connects the grcp connection with a io.ReadWriter.
//
// Blocks until one connection is closed.
func copyStream(sr sendReceiver, rw io.ReadWriter) error {
	// Create channels to send errors from the goroutines. The channels have to
	// be buffered. If an error in one goroutine happens this function exists.
	// If this happens, the other goroutine still runs. If an error happens, it
	// is wirtten to the channel. An unblocked channel would block forever.
	fromGRPC := make(chan error, 1)
	fromRW := make(chan error, 1)

	// Message from gRPC
	go func() {
		defer close(fromGRPC)
		if err := receiverToWriter(rw, sr); err != nil {
			fromGRPC <- fmt.Errorf("copy data from gRPC: %w", err)
		}
	}()

	// Message to gRPC
	go func() {
		defer close(fromRW)

		if err := readerToSender(sr, rw); err != nil {
			fromRW <- fmt.Errorf("copy data to gRPC: %w", err)
		}
	}()

	// Wait for one side to finish.
	select {
	case err := <-fromGRPC:
		if err != nil {
			return fmt.Errorf("from grpc: %w", err)
		}
	case err := <-fromRW:
		if err != nil {
			return fmt.Errorf("from read writer: %w", err)
		}
	}
	return nil
}

// receiverToWriter copies the data from the receiver (gRPC tunnel) to a writer.
func receiverToWriter(w io.Writer, r receiver) error {
	for {
		c, err := r.Recv()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("receiving data: %w", err)
		}

		if _, err := w.Write(c.Data); err != nil {
			return fmt.Errorf("writing data: %w", err)
		}
	}
}

//rederToSender copys data from the reader to the sender (gPRC tunnel).
func readerToSender(s sender, r io.Reader) error {
	buff := make([]byte, 1<<20) // 1 MB buffer
	for {
		n, err := r.Read(buff)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("receiving data: %w", err)
		}

		if err := s.Send(&proto.TunnelData{Data: buff[:n]}); err != nil {
			return fmt.Errorf("writing data: %w", err)
		}
	}
}
