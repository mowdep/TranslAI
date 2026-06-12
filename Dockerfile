# Multi-stage : base (toolchain) → test (gate) → build (binaire) → runtime (distroless).

############################ base : toolchain Go + lint ############################
FROM golang:1.23-alpine AS base
RUN apk add --no-cache git make bash
# golangci-lint v1.64 = compatible avec le format de .golangci.yml (v1).
RUN go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.8
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

############################ test : gate complet ############################
# Echoue le build si vet/lint/test/build rouge. `docker build --target test`.
FROM base AS test
RUN make check

############################ build : binaire statique ############################
FROM base AS build
RUN CGO_ENABLED=0 GOOS=linux go build \
      -ldflags="-s -w" \
      -o /translai \
      ./cmd/translai

############################ runtime : image finale minimale ############################
FROM gcr.io/distroless/static-debian12
COPY --from=build /translai /translai

# /config persiste config.yaml et les snapshots de jobs (WorkDir).
VOLUME ["/config"]
EXPOSE 8080

ENTRYPOINT ["/translai"]
CMD ["web", "--addr", ":8080", "--config", "/config/config.yaml"]
