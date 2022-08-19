# FuzzerMan
## A Simple Manager for distributed running of static libFuzzer binaries

This is just a minimal mananger service for statically linked libFuzzer binaries. Basically just an implementation of the "Distributed Fuzzing" section of [Google's libFuzzerTutorial](https://github.com/google/fuzzing/blob/master/tutorial/libFuzzerTutorial.md#distributed-fuzzing).

Main offering is to be able to spin this up as a docker container on fuzzing machines and have those instances automatically download/update the target binary and fuzz it. Then upload logs, artifacts, and any new corpus. I've also implemented a basic corpus merge system so one of the instances will occasionally attempt to merge the corpus and update it for everyone else.

## Crash Reporting

There is a basic crash reporter built in. Once a crash is encounted a `multipart/form-data` POST request will be made to the `ReportingEndpoint` in the configuration file. The body of this request will have two fields `log` and `artifact` containing the contents of the log and artifact files respectively.

No server-side implementation is provided for this. Its meant to be flexible for you to treat those crashes however you want, but this way you can get instant notification of crashes and do some minor processing on them.

## Configuration

All configuration is through a JSON file. The format of the configuration file is documented in [pkg/config/config.go](pkg/config/config.go)

## Running

The only argument is `-config` to provide the path to the configuration JSON file. 