# Stacker

A tool to rebase container (Docker) images instead of rebuilding them.

It is based around the idea of adding OCI annotations for base images to just replace the layers of the base image if a newer version is available.
Have a look at [crane rebase](https://github.com/google/go-containerregistry/blob/main/cmd/crane/rebase.md) for more information.

This enables updates of app images without needing a complete build and is useful e.g. when running many different apps based on the same base image(s).

## Usage

### Requirements

- You need to build your app images with OCI base image annotations (e.g. with [crane append --set-base-image-annotations](https://github.com/google/go-containerregistry/blob/main/cmd/crane/doc/crane_append.md))
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

```
  ◦ Rebasing                  file=config/my-app.yaml image=registry.example.com/my-group/my-project tag=2.3.0
  ◦ Pushing rebased image as  file=config/my-app.yaml image=registry.example.com/my-group/my-project tag=2.3.0 rebased=registry.example.com/my-group/my-project:2.3.0
  • Rebased image             file=config/my-app.yaml image=registry.example.com/my-group/my-project tag=2.3.0 newDigest=sha256:b8a1c2638173eb55dc30489cc0a1beb60680b5838d9eddb34bf9213ec73d3d1e
  • Wrote back updated YAML to file file=config/my-app.yaml
```

After running the command, the rebased image will be pushed and the YAML file will be updated to:

```yaml
# ...
spec:
  # ...
  values:
    app:
      image:
        repository: registry.example.com/my-group/my-project # {"$rebase": "app:name"}
        tag: 2.3.0@sha256:b8a1c2638173eb55dc30489cc0a1beb60680b5838d9eddb34bf9213ec73d3d1e # {"$rebase": "app:tag"}
```

The next step is to push the changes back to your Git repository, so a GitOps operator like Flux can apply the changes.

Ideally this is done in a scheduled CI pipeline to keep the images up-to-date.

### CLI

```
NAME:
   stacker - Automatic rebasing of images using OCI base image annotations

USAGE:
   cmd [global options] command [command options] [directory]

DESCRIPTION:
   Recurses through the given directory to find YAML files with a rebase annotation and rebases the image onto the newest base image.

COMMANDS:
   help, h  Shows a list of commands or help for one command

GLOBAL OPTIONS:
   --help, -h             show help (default: false)
   --super-verbose, --vv  Enable super verbose logging (default: false)
   --verbose, -v          Enable verbose logging (default: false)
```

## Acknowledgements

This tool is based on the code from https://github.com/google/go-containerregistry/tree/main/cmd/crane for the image manipulation.

## License

[MIT](./LICENSE)
