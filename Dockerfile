FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
RUN go test ./... && CGO_ENABLED=0 go build -ldflags="-s -w" -o /gateway .

FROM scratch
COPY --from=build /gateway /gateway
EXPOSE 8080
USER 65534:65534
ENTRYPOINT ["/gateway"]
