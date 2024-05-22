package yaml_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/networkteam/stacker/yaml"
)

func TestNewProcessor(t *testing.T) {
	r := strings.NewReader("key: value")
	processor, err := yaml.NewProcessor(r)
	require.NoError(t, err)
	assert.NotNil(t, processor)
}

func TestNewProcessor_InvalidYAML(t *testing.T) {
	r := strings.NewReader("key: value: invalid")
	_, err := yaml.NewProcessor(r)
	assert.Error(t, err)
}

func TestProcessor_Encode(t *testing.T) {
	r := strings.NewReader("key: value # hey there")
	processor, _ := yaml.NewProcessor(r)
	var sb strings.Builder
	err := processor.Encode(&sb)
	require.NoError(t, err)
	assert.Equal(t, "key: value # hey there\n", sb.String())
}

func TestRebaseAnnotation_TagWithoutDigest(t *testing.T) {
	annotation := yaml.RebaseAnnotation{Tag: "1.2.3@sha256:d7500ff35777c1835490fb5d4bd5283236c9d18cdc59858c3203eda82abab412"}
	assert.Equal(t, "1.2.3", annotation.TagWithoutDigest())
}

func TestRebaseAnnotation_TagWithoutDigest_NoDigest(t *testing.T) {
	annotation := yaml.RebaseAnnotation{Tag: "task-refactor"}
	assert.Equal(t, "task-refactor", annotation.TagWithoutDigest())
}

func TestProcessor_FindRebaseAnnotations(t *testing.T) {
	r := strings.NewReader(`
app:
  image: my.registry.com/project/app # {"$rebase": "my-app:name"}
  tag: 1.2.3 # {"$rebase": "my-app:tag"}
`)
	processor, _ := yaml.NewProcessor(r)
	annotations, err := processor.FindRebaseAnnotations()
	require.NoError(t, err)
	require.Len(t, annotations, 1)
	assert.Equal(t, "my-app", annotations[0].Identifier)
	assert.Equal(t, "my.registry.com/project/app", annotations[0].Name)
	assert.Equal(t, "1.2.3", annotations[0].Tag)
}

func TestProcessor_FindRebaseAnnotations_InvalidJSON(t *testing.T) {
	r := strings.NewReader("key: value # {\"$rebase\": \"id:name\",}")
	processor, _ := yaml.NewProcessor(r)
	_, err := processor.FindRebaseAnnotations()
	assert.Error(t, err)
}

func TestProcessor_FindRebaseAnnotations_NoRebase(t *testing.T) {
	r := strings.NewReader("key: value # {\"$other\": \"id:name\"}")
	processor, _ := yaml.NewProcessor(r)
	annotations, err := processor.FindRebaseAnnotations()
	require.NoError(t, err)
	assert.Empty(t, annotations)
}

func TestProcessor_FindRebaseAnnotations_InvalidRebaseValue(t *testing.T) {
	r := strings.NewReader("key: value # {\"$rebase\": \"id\"}")
	processor, _ := yaml.NewProcessor(r)
	_, err := processor.FindRebaseAnnotations()
	assert.Error(t, err)
}

func TestProcessor_FindRebaseAnnotations_InvalidRebasePart(t *testing.T) {
	r := strings.NewReader("key: value # {\"$rebase\": \"id:invalid\"}")
	processor, _ := yaml.NewProcessor(r)
	_, err := processor.FindRebaseAnnotations()
	assert.Error(t, err)
}
