# This configuration was created from a template file. The accompanying services.env file
# might be the correct place for customizations.

version: '3.4'

services:
  proxy:
    {{ .Service.proxy }}
    depends_on:
      - client
      - backend
      - autoupdate
      - auth
      - media
    env_file: services.env
    networks:
      - uplink
      - frontend
    ports:
      - "127.0.0.1:{{ .ExternalHTTPPort }}:8000"

  client:
    {{ .Service.client }}
    depends_on:
      - backend
      - autoupdate
    networks:
      - frontend

  backend:
    {{ .Service.backend }}
    depends_on:
      - datastore-reader
      - datastore-writer
      - auth
    env_file: services.env
    networks:
      - frontend
      - backend
    secrets:
      - auth_token_key
      - auth_cookie_key

  datastore-reader:
    {{ .Service.datastore_reader }}
    depends_on:
      - postgres
    env_file: services.env
    environment:
      - NUM_WORKERS=8
    networks:
      - backend
      - datastore-reader
      - postgres

  datastore-writer:
    {{ .Service.datastore_writer }}
    depends_on:
      - postgres
      - message-bus
    env_file: services.env
    networks:
      - backend
      - postgres
      - message-bus

  postgres:
    image: postgres:11
    environment:
      - POSTGRES_USER=openslides
      - POSTGRES_PASSWORD=openslides
      - POSTGRES_DB=openslides
      - PGDATA=/var/lib/postgresql/data/pgdata
    networks:
      - postgres
    volumes:
      - ./db-data:/var/lib/postgresql/data

  autoupdate:
    {{ .Service.autoupdate }}
    depends_on:
      - datastore-reader
      - message-bus
    env_file: services.env
    networks:
      - frontend
      - datastore-reader
      - message-bus
    secrets:
      - auth_token_key
      - auth_cookie_key

  auth:
    {{ .Service.auth }}
    depends_on:
      - datastore-reader
      - message-bus
      - cache
    env_file: services.env
    networks:
      - frontend
      - datastore-reader
      - message-bus
      - cache
    secrets:
      - auth_token_key
      - auth_cookie_key

  cache:
    image: redis:latest
    networks:
      - cache

  message-bus:
    image: redis:latest
    networks:
      - message-bus

  media:
    {{ .Service.media }}
    depends_on:
      - backend
      - postgres
    env_file: services.env
    networks:
      - frontend
      - backend
      - postgres

  icc:
    {{ .Service.icc }}
    depends_on:
      - datastore-reader
      - message-bus
      - auth
    env_file: services.env
    networks:
      - frontend
      - datastore-reader
      - message-bus
    secrets:
      - auth_token_key
      - auth_cookie_key

  manage:
    {{ .Service.manage }}
    depends_on:
      - datastore-reader
      - datastore-writer
      - auth
    env_file: services.env
    ports:
      - "127.0.0.1:{{ .ExternalManagePort }}:9008"
    networks:
      - uplink
      - frontend
      - backend
    secrets:
      - admin

# TODO: Remove this service so the networks won't matter any more.
  permission:
    {{ .Service.permission }}
    depends_on:
    - datastore-reader
    env_file: services.env
    networks:
    - frontend
    - backend

# Setup: host <-uplink-> proxy <-frontend-> services that are reachable from the client <-backend-> services that are internal-only
# There are special networks for some services only, e.g. postgres only for the postgresql, datastore reader and datastore writer
networks:
  uplink:
  frontend:
    internal: true
  backend:
    internal: true
  postgres:
    internal: true
  datastore-reader:
    internal: true
  message-bus:
    internal: true
  cache:
    internal: true

secrets:
  auth_token_key:
    file: ./secrets/auth_token_key
  auth_cookie_key:
    file: ./secrets/auth_cookie_key
  admin:
    file: ./secrets/admin
