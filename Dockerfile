FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /pingr .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=build /pingr /pingr
COPY urls.txt /urls.txt
EXPOSE 9100
ENTRYPOINT ["/pingr", "-f", "/urls.txt"]
CMD ["-serve", ":9100"]
