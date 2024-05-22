# Stacker

A tool to rebase container (Docker) images instead of rebuilding them.

It is based around the idea of OCI annotations for base images to just change the layers of the base image if a newer version is available.

## Usage

### Requirements

- You need to build your app images with OCI base image annoations (e.g. with [crane rebase](https://github.com/google/go-containerregistry/blob/main/cmd/crane/rebase.md))
- The `FROM` image in your app Dockerfile should point to a base image tag that is updated as an alias (e.g. `:1` or `:latest`)
- You need to include `{"$rebase": "[identifier]:name"}` and `{"$rebase": "[identifier]:tag"}` annotations as comments in a YAML file, where `identifier` needs to be unique per file

### Example

Take this example of rebasing images inside a [Flux](https://fluxcd.io/) `HelmRelease`:

**config/my-app.yaml**

```yaml
apiVersion: helm.toolkit.fluxcd.io/v2beta1
kind: HelmRelease
metadata:
  name: integration
spec:
  chart:
    spec:
      chart: my-app
      reconcileStrategy: ChartVersion
      sourceRef:
        kind: HelmRepository
        name: my-app
        namespace: my-namespace
      version: '1.1.0'
  interval: 5m0s
  releaseName: integration
  values:
    app:
      image:
        repository: registry.example.com/my-group/my-project # {"$rebase": "app:name"}
        tag: 2.3.0 # {"$rebase": "app:tag"}
```

By using the `$rebase` annotation, Stacker will rebase the given image onto the latest base image and update the YAML tag to include the new digest.

```shell
stacker ./config
```

After running the command, the file will be updated to:

```yaml
# ...
spec:
  # ...
  values:
    app:
      image:
        repository: registry.example.com/my-group/my-project # {"$rebase": "app:name"}
        tag: 2.3.0@sha256:bcccf02bf92e0c766761cc7d5a1faee70b6b07578f7173d640970fe9ef6571ae # {"$rebase": "app:tag"}
```

## Acknowledgements

This tool heavily uses https://github.com/google/go-containerregistry/tree/main/cmd/crane for the image manipulation.

## License

[MIT](./LICENSE)
