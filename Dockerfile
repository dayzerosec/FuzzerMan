FROM golang:latest as builder
COPY ./cmd /app/cmd
COPY ./pkg /app/pkg
COPY ./go.* /app/
WORKDIR /app
RUN CGO_ENABLED=0 go build -a -installsuffix cgo -o FuzzerMan ./cmd/FuzzerMan

FROM ubuntu:latest

ARG LLVM_VER=14
RUN apt-get update && apt-get -y install ca-certificates wget bash lsb-release software-properties-common
RUN apt-get -y install curl
RUN curl -o /llvm.sh https://apt.llvm.org/llvm.sh && chmod +x /llvm.sh && /llvm.sh $LLVM_VER && rm /llvm.sh
ENV PATH="/usr/lib/llvm-${LLVM_VER}/bin:${PATH}"

COPY --from=builder /app/FuzzerMan /app/FuzzerMan
ENTRYPOINT ["/app/FuzzerMan"]