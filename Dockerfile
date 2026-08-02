FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
COPY public/ ./public/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/host . \
    && install -d -o 65532 -g 65532 /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/host /host
COPY --from=build --chown=65532:65532 /out/data /data
USER nonroot
VOLUME ["/data"]
ENV ADDR=":8080" DATA_DIR="/data"
EXPOSE 8080
ENTRYPOINT ["/host"]
