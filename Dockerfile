# Argument for Go version
ARG GO_VERSION
ARG TEST_CONFIG
# STAGE 1: building the executable
FROM golang:${GO_VERSION}-alpine AS build

WORKDIR /src
COPY ./go.mod ./go.sum ./
RUN go mod download               \
    && echo ${TEST_CONFIG} > config.json
COPY ./ ./

# Build the executable
RUN CGO_ENABLED=0                 \
    go build                      \
    -a -tags netgo -ldflags '-w'  \
    -o /app main.go
 
# STAGE 2: build the container to run
FROM gcr.io/distroless/static AS final
 
LABEL maintainer="asiantbd_team"

USER nonroot:nonroot
# copy compiled app
COPY --from=build --chown=nonroot:nonroot /app /app
COPY --from=build --chown=nonroot:nonroot /src/config.json /app/config.json
WORKDIR /app
# run binary; use vector form
ENTRYPOINT ["/app"]
