FROM golang:latest as builder
COPY ./cmd /app/cmd
COPY ./pkg /app/pkg
COPY ./go.* /app/
WORKDIR /app
RUN CGO_ENABLED=0 go build -a -installsuffix cgo -o FuzzerMan ./cmd/FuzzerMan

FROM ubuntu:latest

COPY --from=builder /app/FuzzerMan /app/FuzzerMan
ENTRYPOINT ["/app/FuzzerMan"]

ARG LLVM_VER=14
RUN apt-get update && apt-get -y install ca-certificates wget bash lsb-release software-properties-common
RUN apt-get -y install curl
RUN curl -o /llvm.sh https://apt.llvm.org/llvm.sh && chmod +x /llvm.sh && /llvm.sh $LLVM_VER && rm /llvm.sh
ENV PATH="/usr/lib/llvm-${LLVM_VER}/bin:${PATH}"

RUN echo "deb [signed-by=/usr/share/keyrings/cloud.google.gpg] https://packages.cloud.google.com/apt cloud-sdk main" | tee -a /etc/apt/sources.list.d/google-cloud-sdk.list
RUN curl https://packages.cloud.google.com/apt/doc/apt-key.gpg | apt-key --keyring /usr/share/keyrings/cloud.google.gpg add -
RUN curl https://packages.cloud.google.com/apt/doc/apt-key.gpg | apt-key add -
RUN apt-get update && apt-get install -y google-cloud-cli
