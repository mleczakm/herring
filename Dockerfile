# syntax=docker/dockerfile:1
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /herring ./cmd/herring

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /herring /herring
EXPOSE 8080 8090
USER nonroot:nonroot
ENTRYPOINT ["/herring"]
