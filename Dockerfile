# syntax=docker/dockerfile:1
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /herring ./cmd/herring
RUN mkdir /data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /herring /herring
COPY --from=build --chown=nonroot:nonroot /data /data
VOLUME ["/data"]
ENV HERRING_DATABASE_PATH=/data/herring.db
EXPOSE 8080 8090
USER nonroot:nonroot
ENTRYPOINT ["/herring"]
