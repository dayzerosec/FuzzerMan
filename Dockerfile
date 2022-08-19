FROM golang:latest as builder
COPY ./cmd /app/cmd
COPY ./pkg /app/pkg
COPY ./go.* /app/
WORKDIR /app
RUN CGO_ENABLED=0 go build -a -installsuffix cgo -o FuzzerMan ./cmd/FuzzerMan

FROM alpine:latest
COPY --from=builder /app/FuzzerMan /app/FuzzerMan
ENTRYPOINT ["/app/FuzzerMan"]