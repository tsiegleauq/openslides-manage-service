package manage

import (
	"fmt"
	"os"
	"os/exec"
	"path"

	"github.com/adrg/xdg"
	"github.com/spf13/cobra"
)

const helpCompose = `Calls docker-compose TODO

TODO
`

// CmdCompose calls docker-compose.
// TODO: Check and maybe refactor this code. Use --local flag and other ...
func CmdCompose(cfg *ClientConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "compose",
		Short: "Calls a docker-compose command",
		Long:  helpCompose,
	}

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		dataPath := path.Join(xdg.DataHome, appName)

		composeArgs := []string{"-f", dataPath + "/docker-compose.yml"}
		composeArgs = append(composeArgs, args...)

		dockerCompose := exec.Command("docker-compose", composeArgs...)
		dockerCompose.Stdin = os.Stdin
		dockerCompose.Stdout = os.Stdout
		dockerCompose.Stderr = os.Stderr
		dockerCompose.Dir = dataPath

		if err := dockerCompose.Run(); err != nil {
			// TODO: Print complete command here
			return fmt.Errorf("running docker-compose: %w", err)
		}

		return nil
	}

	return cmd
}
