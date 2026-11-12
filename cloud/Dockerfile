FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -o /out/api ./cmd/api
RUN mkdir -p /out/pulse-data && touch /out/pulse-data/.volume-init

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/api /api
COPY --from=build --chown=65532:65532 /out/pulse-data /var/lib/pulse-mesh
EXPOSE 8080
ENTRYPOINT ["/api"]
