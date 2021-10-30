# argument for Go version
ARG GO_VERSION=1.16.4
ARG APPLICATION_NAME
# STAGE 1: building the executable
FROM golang:${GO_VERSION}-alpine AS build
 
WORKDIR /src
COPY ./go.mod ./go.sum ./
RUN go mod download
COPY ./ ./

# Build the executable
RUN CGO_ENABLED=0 \
    go build \
    -a -tags netgo -ldflags '-w' \
    -o /${APPLICATION_NAME} main.go
 
# STAGE 2: build the container to run
FROM gcr.io/distroless/static AS final
 
LABEL maintainer="guaychou"

USER nonroot:nonroot
# copy compiled app
COPY --from=build --chown=nonroot:nonroot /${APPLICATION_NAME} /${APPLICATION_NAME}
 
# run binary; use vector form
ENTRYPOINT ["/${APPLICATION_NAME}"]
