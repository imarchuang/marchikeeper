# syntax=docker/dockerfile:1

FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY internal ./internal
COPY cmd ./cmd
RUN go test ./... && go build -o /out/marchikeeper ./cmd/marchikeeper

FROM alpine:3.20
WORKDIR /app
COPY --from=build /out/marchikeeper /usr/local/bin/marchikeeper
EXPOSE 7181
ENTRYPOINT ["marchikeeper"]
CMD ["-listen", ":7181"]
