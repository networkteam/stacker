# Stacker

A tool to rebase container (Docker) images instead of rebuilding them.

It is based around the idea of OCI annotations for base images to just change the layers of the base image if a newer version is available.

## Acknowledgements

This tool heavily uses https://github.com/google/go-containerregistry/tree/main/cmd/crane for the image manipulation.

## License

[MIT](./LICENSE)
