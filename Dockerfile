FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
COPY tokens.css ./
COPY public/ ./public/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/hoard . \
    && install -d -o 65532 -g 65532 /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/hoard /hoard
COPY --from=build --chown=65532:65532 /out/data /data
USER nonroot
VOLUME ["/data"]
ENV ADDR=":8080" DATA_DIR="/data"
EXPOSE 8080
ENTRYPOINT ["/hoard"]
