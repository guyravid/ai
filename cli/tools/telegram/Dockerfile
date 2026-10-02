# telegram in a minimal container: a static binary on distroless, running as a non-root user.
#
#   docker build -t telegram-ro .                         read-only build (default)
#   docker build --build-arg TAGS=mcp -t telegram .       full build, with write commands
#
# The nonroot user has no home directory, so the tool will not guess its paths: config and
# datasets are set explicitly below. Mount /data writable; mount the config read-only.

FROM golang:1.25 AS build
ARG TAGS=mcp,readonly
ARG VERSION=0.1.0-dev
ARG COMMIT=unknown
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -tags "${TAGS}" \
      -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" -o /out/telegram .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/telegram /telegram
ENV TELEGRAM_CONFIG=/config/config.json \
    TELEGRAM_DATASET_DIR=/data/datasets
VOLUME /data
USER nonroot:nonroot
ENTRYPOINT ["/telegram"]
