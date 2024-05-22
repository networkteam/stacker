package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/hashicorp/go-multierror"
	"github.com/networkteam/slogutils"
	specsv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/urfave/cli/v2"

	"github.com/networkteam/stacker/yaml"
)

func main() {
	app := cli.NewApp()
	app.Name = "stacker"
	app.Usage = "Automatic rebasing of images using OCI base image annotations"
	app.Flags = []cli.Flag{
		&cli.BoolFlag{
			Name:    "verbose",
			Aliases: []string{"v"},
			Usage:   "Enable verbose logging",
		},
		&cli.BoolFlag{
			Name:    "super-verbose",
			Aliases: []string{"vv"},
			Usage:   "Enable super verbose logging",
		},
	}
	app.ArgsUsage = "[directory]"
	app.Before = func(c *cli.Context) error {
		level := slog.LevelInfo
		if c.Bool("verbose") {
			level = slog.LevelDebug
		} else if c.Bool("super-verbose") {
			level = slogutils.LevelTrace
		}

		slog.SetDefault(slog.New(
			slogutils.NewCLIHandler(os.Stderr, &slogutils.CLIHandlerOptions{
				Level: level,
			}),
		))

		return nil
	}
	app.Description = "Recurses through the given directory to find YAML files with a rebase annotation and rebases the image onto the newest base image."
	app.Action = func(c *cli.Context) error {
		if c.NArg() != 1 {
			return fmt.Errorf("expected exactly one directory argument")
		}

		directory := c.Args().First()

		var rebaseErr error

		// Find YAML files in directory
		err := filepath.Walk(directory, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			if info.IsDir() {
				return nil
			}

			if filepath.Ext(path) != ".yaml" && filepath.Ext(path) != ".yml" {
				return nil
			}

			// Get path relative to directory
			relPath, err := filepath.Rel(directory, path)
			if err != nil {
				return fmt.Errorf("getting relative path: %w", err)
			}

			ctx := slogutils.WithLogger(c.Context, slog.With("file", relPath))

			err = processRebaseAnnotations(ctx, path)
			if err != nil {
				rebaseErr = multierror.Append(rebaseErr, fmt.Errorf("processing %s: %w", relPath, err))
			}

			return nil
		})
		if err != nil {
			return multierror.Append(rebaseErr, fmt.Errorf("walking directory: %w", err))
		}

		return rebaseErr
	}

	err := app.Run(os.Args)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func processRebaseAnnotations(ctx context.Context, path string) error {
	logger := slogutils.FromContext(ctx)

	logger.Log(ctx, slogutils.LevelTrace, "Checking for annotations in file")

	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("opening file: %w", err)
	}
	defer f.Close()

	patcher, err := yaml.NewProcessor(f)
	if err != nil {
		return fmt.Errorf("opening YAML: %w", err)
	}

	annotations, err := patcher.FindRebaseAnnotations()
	if err != nil {
		return fmt.Errorf("finding rebase annotations: %w", err)
	}

	// Process annotations

	var rebaseErr error
	var didRebaseAny bool

	for _, annotation := range annotations {
		newDigest, didRebase, err := processRebaseAnnotation(ctx, annotation)
		if err != nil {
			rebaseErr = multierror.Append(rebaseErr, fmt.Errorf("rebasing image %s:%s: %w", annotation.Name, annotation.TagWithoutDigest(), err))
		}
		if !didRebase {
			logger.Debug("No-op rebase", "image", annotation.Name, "tag", annotation.TagWithoutDigest())
			continue
		}

		logger.Info("Rebased image", "image", annotation.Name, "tag", annotation.TagWithoutDigest(), "newDigest", newDigest)
		didRebaseAny = true

		annotation.UpdateTagDigest(newDigest)
	}

	if didRebaseAny {
		// Write back to file by calling Encode from patcher to file

		err = f.Truncate(0)
		if err != nil {
			return fmt.Errorf("truncating file: %w", err)
		}
		_, err = f.Seek(0, 0)
		if err != nil {
			return fmt.Errorf("seeking to beginning of file: %w", err)
		}

		err = patcher.Encode(f)
		if err != nil {
			return fmt.Errorf("encoding YAML back to file: %w", err)
		}

		logger.Info("Wrote back updated YAML to file")
	}

	return rebaseErr
}

func processRebaseAnnotation(ctx context.Context, annotation yaml.RebaseAnnotation) (string, bool, error) {
	logger := slogutils.FromContext(ctx).With("image", annotation.Name, "tag", annotation.TagWithoutDigest())

	logger.Debug("Rebasing")

	var oldBase, newBase, rebased string

	orig := fmt.Sprintf("%s:%s", annotation.Name, annotation.TagWithoutDigest())
	// For now the target is always the same image and tag
	rebased = orig

	r, err := name.ParseReference(rebased)
	if err != nil {
		return "", false, fmt.Errorf("parsing rebased reference: %w", err)
	}

	desc, err := crane.Head(orig)
	if err != nil {
		return "", false, fmt.Errorf("checking: %w", err)
	}

	if desc.MediaType.IsIndex() {
		return "", false, errors.New("rebasing an index is not yet supported")
	}

	// This is from `crane rebase`

	origImg, err := crane.Pull(orig)
	if err != nil {
		return "", false, fmt.Errorf("pulling image: %w", err)
	}
	origMf, err := origImg.Manifest()
	if err != nil {
		return "", false, fmt.Errorf("getting manifest: %w", err)
	}
	anns := origMf.Annotations
	newBase = anns[specsv1.AnnotationBaseImageName]
	if newBase == "" {
		return "", false, errors.New("could not determine new base image from annotations")
	}
	newBaseRef, err := name.ParseReference(newBase)
	if err != nil {
		return "", false, fmt.Errorf("parsing new base reference: %w", err)
	}
	oldBaseDigest := anns[specsv1.AnnotationBaseImageDigest]
	oldBase = newBaseRef.Context().Digest(oldBaseDigest).String()
	if oldBase == "" {
		return "", false, errors.New("could not determine old base image by digest from annotations")
	}

	rebasedImg, err := rebaseImage(ctx, origImg, oldBase, newBase)
	if err != nil {
		return "", false, fmt.Errorf("rebasing image: %w", err)
	}

	rebasedDigest, err := rebasedImg.Digest()
	if err != nil {
		return "", false, fmt.Errorf("digesting new image: %w", err)
	}
	origDigest, err := origImg.Digest()
	if err != nil {
		return "", false, fmt.Errorf("digesting old image: %w", err)
	}

	// Check if the image was rebased or we had a no-op rebase
	if rebasedDigest == origDigest {
		return rebasedDigest.String(), false, nil
	}

	if _, ok := r.(name.Digest); ok {
		rebased = r.Context().Digest(rebasedDigest.String()).String()
	}

	logger.Debug("Pushing rebased image as", "rebased", rebased)
	err = crane.Push(rebasedImg, rebased)
	if err != nil {
		return "", false, fmt.Errorf("pushing %s: %v", rebased, err)
	}

	return rebasedDigest.String(), true, nil
}

// rebaseImage parses the references and uses them to perform a rebase on the
// original image.
//
// If oldBase or newBase are "", rebaseImage attempts to derive them using
// annotations in the original image. If those annotations are not found,
// rebaseImage returns an error.
//
// If rebasing is successful, base image annotations are set on the resulting
// image to facilitate implicit rebasing next time.
func rebaseImage(ctx context.Context, orig v1.Image, oldBase, newBase string, opt ...crane.Option) (v1.Image, error) {
	logger := slogutils.FromContext(ctx)

	m, err := orig.Manifest()
	if err != nil {
		return nil, err
	}
	if newBase == "" && m.Annotations != nil {
		newBase = m.Annotations[specsv1.AnnotationBaseImageName]
		if newBase != "" {
			logger.Log(ctx, slogutils.LevelTrace, "Detected new base from annotation", "annotationName", specsv1.AnnotationBaseImageName, "newBase", newBase)
		}
	}
	if newBase == "" {
		return nil, fmt.Errorf("either new base or %q annotation is required", specsv1.AnnotationBaseImageName)
	}
	newBaseImg, err := crane.Pull(newBase, opt...)
	if err != nil {
		return nil, err
	}

	if oldBase == "" && m.Annotations != nil {
		oldBase = m.Annotations[specsv1.AnnotationBaseImageDigest]
		if oldBase != "" {
			newBaseRef, err := name.ParseReference(newBase)
			if err != nil {
				return nil, fmt.Errorf("parsing new base reference: %w", err)
			}

			oldBase = newBaseRef.Context().Digest(oldBase).String()
			logger.Log(ctx, slogutils.LevelTrace, "Detected old base from annotation", "annotationName", specsv1.AnnotationBaseImageDigest, "oldBase", oldBase)
		}
	}
	if oldBase == "" {
		return nil, fmt.Errorf("either old base or %q annotation is required", specsv1.AnnotationBaseImageDigest)
	}

	oldBaseImg, err := crane.Pull(oldBase, opt...)
	if err != nil {
		return nil, fmt.Errorf("pulling old base image: %w", err)
	}

	// NB: if newBase is an index, we need to grab the index's digest to
	// annotate the resulting image, even though we pull the
	// platform-specific image to rebase.
	// crane.Digest will pull a platform-specific image, so use crane.Head
	// here instead.
	newBaseDesc, err := crane.Head(newBase, opt...)
	if err != nil {
		return nil, fmt.Errorf("getting new base image digest: %w", err)
	}
	newBaseDigest := newBaseDesc.Digest.String()

	rebased, err := mutate.Rebase(orig, oldBaseImg, newBaseImg)
	if err != nil {
		return nil, fmt.Errorf("rebasing: %w", err)
	}

	// Update base image annotations for the new image manifest.
	logger.Log(ctx, slogutils.LevelTrace, "Setting annotation", "annotationName", specsv1.AnnotationBaseImageDigest, "annotationValue", newBaseDigest)
	logger.Log(ctx, slogutils.LevelTrace, "Setting annotation", "annotationName", specsv1.AnnotationBaseImageName, "annotationValue", newBase)
	return mutate.Annotations(rebased, map[string]string{
		specsv1.AnnotationBaseImageDigest: newBaseDigest,
		specsv1.AnnotationBaseImageName:   newBase,
	}).(v1.Image), nil
}
