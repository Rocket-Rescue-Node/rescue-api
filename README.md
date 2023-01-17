[![golangci-lint](https://github.com/Rocket-Pool-Rescue-Node/rescue-proxy/actions/workflows/golangci-lint.yml/badge.svg)](https://github.com/Rocket-Pool-Rescue-Node/rescue-proxy/actions/workflows/golangci-lint.yml)

# Rescue-API

The Rescue-API allows Node Operators to request credentials for the [Rescue Node](https://rescuenode.com)
([GitHub](https://github.com/Rocket-Rescue-Node/rescue-proxy)).


## Building

These instructions will get you a copy of the project up and running on your local
machine for development and testing purposes.

### Requirements

  * [Go](https://golang.org/doc/install) 1.19 or higher
  * [GNU Make](https://www.gnu.org/software/make/)

### Build

```bash
make build
```

## Usage

```
Usage of ./rescue-api:
  -addr string
        Address on which to listen to HTTP requests (default "0.0.0.0:8080")
  -allowed-origins string
        Comma-separated list of allowed CORS origins (default "localhost")
  -auth-valid-for string
        The duration after which a credential should be considered invalid, eg, 360h for 15 days (default "360h")
  -db-path string
        sqlite3 database path (default "db.sqlite3")
  -debug
        Whether to enable verbose logging
  -hmac-secret string
        The secret to use for HMAC (default "test-secret")
  -rescue-proxy-api-addr string
        Address for the Rescue Proxy gRPC API
  -rocketscan-api-url string
        URL for the Rocketscan REST API
```

  * `-hmac-secret` must match the one used with the
  [Credentials](https://github.com/Rocket-Pool-Rescue-Node/credentials) library
  that generated the username, password

## Docker

If you need to publish a new version of the Docker image, you can use the following
commands. You will need to be logged into Docker Hub.

### Requirements

  * [GNU Make](https://www.gnu.org/software/make/)
  * [Docker](https://docs.docker.com/get-docker/)

### Prepare the build environment

This step is needed only once.

```bash
make docker-prepare
```

### Build and publish the image

The following commands will build and publish the image with the specified version.
If you need to create a multi-arch image, you can edit `Makefile` and add
additional architectures to the `PLATFORMS` variable.

```bash
VERSION=<version> make docker-publish
```

## Contributing

Pull requests are welcome. For major changes, please open an issue first
to discuss what you would like to change.

Please make sure to update tests as appropriate.

## License

[AGPL](https://www.gnu.org/licenses/agpl-3.0.en.html)
Copyright (C) 2022 Jacob Shufro and João Poupino

