# Build the server binary.
FROM golang:1.20-alpine AS build

WORKDIR /src

# Dependencies first, so they stay cached while the sources change.
COPY go.mod go.sum ./
RUN go mod download

COPY appConfig ./appConfig
COPY proto ./proto
COPY tunnel ./tunnel
COPY server ./server

# Static binary: the final image has no libc.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./server

# distroless/static has no shell and no package manager, and runs as an
# unprivileged user.
FROM gcr.io/distroless/static:nonroot

COPY --from=build /out/server /usr/local/bin/server

# Both the control connections from clients and the requests from the web
# arrive here.
EXPOSE 8080

USER nonroot:nonroot

ENTRYPOINT ["/usr/local/bin/server"]

# The server needs a config file. Mount one over this path:
#   -v $PWD/config.json:/etc/mylittleproxy/config.json:ro
# The shared secret is better passed as MYLITTLEPROXY_SIGNATURE_KEY than
# written into it.
CMD ["-c", "/etc/mylittleproxy/config.json"]
