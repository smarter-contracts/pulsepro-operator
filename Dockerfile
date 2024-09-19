# Build the manager binary
FROM golang:1.22 AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY cmd/main.go cmd/main.go
COPY api/ api/
COPY internal/controllers/ internal/controllers/

# Build the manager binary
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o manager cmd/main.go

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot
WORKDIR /

# Install helmfile and dependencies
USER root
RUN apt-get update && apt-get install -y curl gnupg ca-certificates \
  && curl https://raw.githubusercontent.com/helmfile/helmfile/master/scripts/get-helmfile-3 | bash \
  && curl https://raw.githubusercontent.com/helm/helm/master/scripts/get-helm-3 | bash \
  && apt-get clean

USER 65532:65532

COPY --from=builder /workspace/manager .
USER 65532:65532

ENTRYPOINT ["/manager"]
