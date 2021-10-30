# argument for Go version
ARG GO_VERSION
ARG APPLICATION_NAME


# STAGE 1: building the executable
FROM golang:${GO_VERSION}-alpine AS build
ENV GO_VERSION=${GO_VERSION} \
    APPLICATION_NAME=${APPLICATION_NAME}
WORKDIR /src
COPY ./go.mod ./go.sum ./
RUN go mod download
COPY ./ ./

# Build the executable
RUN CGO_ENABLED=0                 \
    go build                      \
    -a -tags netgo -ldflags '-w'  \
    -o /${APPLICATION_NAME} main.go
 
# STAGE 2: build the container to run
FROM gcr.io/distroless/static AS final
 
LABEL maintainer="asiantbd_team"

USER nonroot:nonroot
# copy compiled app
COPY --from=build --chown=nonroot:nonroot /${APPLICATION_NAME} /${APPLICATION_NAME}
 
# run binary; use vector form
ENTRYPOINT ["/${APPLICATION_NAME}"]
