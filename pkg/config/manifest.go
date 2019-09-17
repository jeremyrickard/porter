package config

import (
	"io/ioutil"
	"net/http"
	"reflect"
	"strings"

	multierror "github.com/hashicorp/go-multierror"
	"github.com/pkg/errors"
	yaml "gopkg.in/yaml.v2"

	"github.com/deislabs/cnab-go/bundle/definition"
)

type Manifest struct {
	Name        string `yaml:"name,omitempty"`
	Description string `yaml:"description,omitempty"`
	Version     string `yaml:"version,omitempty"`

	// Image is the name of the invocation image in the format REGISTRY/NAME:TAG
	Image string `yaml:"invocationImage,omitempty"`

	// BundleTag is the name of the bundle in the format REGISTRY/NAME:TAG
	BundleTag string `yaml:"tag"`

	// Dockerfile is the relative path to the Dockerfile template for the invocation image
	Dockerfile string `yaml:"dockerfile,omitempty"`

	Mixins []string `yaml:"mixins,omitempty"`

	Install   Steps `yaml:"install"`
	Uninstall Steps `yaml:"uninstall"`
	Upgrade   Steps `yaml:"upgrade"`

	CustomActions           map[string]Steps                  `yaml:"-"`
	CustomActionDefinitions map[string]CustomActionDefinition `yaml:"customActions,omitempty"`

	Parameters   []ParameterDefinition  `yaml:"parameters,omitempty"`
	Credentials  []CredentialDefinition `yaml:"credentials,omitempty"`
	Dependencies map[string]Dependency  `yaml:"dependencies,omitempty"`
	Outputs      []OutputDefinition     `yaml:"outputs,omitempty"`

	// ImageMap is a map of images referenced in the bundle. If image relocation mapping occurs, that
	// file will be mounted at as a file at runtime to /cnab/app/relocation-mapping.json.
	// TODO: porter should handle the relocation and overwrite the repository and tag (if present), and
	// populate originalImage
	ImageMap map[string]MappedImage `yaml:"images,omitempty"`
}

func (m *Manifest) Validate() error {
	var result error

	if strings.ToLower(m.Dockerfile) == "dockerfile" {
		return errors.New("Dockerfile template cannot be named 'Dockerfile' because that is the filename generated during porter build")
	}

	if len(m.Mixins) == 0 {
		result = multierror.Append(result, errors.New("no mixins declared"))
	}

	if m.Install == nil {
		result = multierror.Append(result, errors.New("no install action defined"))
	}
	err := m.Install.Validate(m)
	if err != nil {
		result = multierror.Append(result, err)
	}

	if m.Uninstall == nil {
		result = multierror.Append(result, errors.New("no uninstall action defined"))
	}
	err = m.Uninstall.Validate(m)
	if err != nil {
		result = multierror.Append(result, err)
	}

	for _, dep := range m.Dependencies {
		err = dep.Validate()
		if err != nil {
			result = multierror.Append(result, err)
		}
	}

	for _, output := range m.Outputs {
		err = output.Validate()
		if err != nil {
			result = multierror.Append(result, err)
		}
	}

	return result
}

// ParameterDefinition defines a single parameter for a CNAB bundle
type ParameterDefinition struct {
	Name      string `yaml:"name"`
	Sensitive bool   `yaml:"sensitive"`

	// These fields represent a subset of bundle.Parameter as defined in deislabs/cnab-go,
	// minus the 'Description' field (definition.Schema's will be used) and `Definition` field
	ApplyTo     []string `yaml:"applyTo,omitempty"`
	Destination Location `yaml:",inline,omitempty"`

	definition.Schema `yaml:",inline"`
}

type CredentialDefinition struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
	Required    bool   `yaml:"required,omitempty"`

	Location `yaml:",inline"`
}

// TODO: use cnab-go's bundle.Location instead, once yaml tags have been added
// Location represents a Parameter or Credential location in an InvocationImage
type Location struct {
	Path                string `yaml:"path,omitempty"`
	EnvironmentVariable string `yaml:"env,omitempty"`
}

func (l Location) IsEmpty() bool {
	var empty Location
	return l == empty
}

type MappedImage struct {
	Description   string            `yaml:"description"`
	ImageType     string            `yaml:"imageType"`
	Repository    string            `yaml:"repository"`
	OriginalImage string            `yaml:"originalImage,omitempty"`
	Digest        string            `yaml:"digest,omitempty"`
	Size          uint64            `yaml:"size,omitempty"`
	MediaType     string            `yaml:"mediaType,omitempty"`
	Labels        map[string]string `yaml:"labels,omitempty"`
	Tag           string            `yaml:"tag,omitempty"`
}

type Dependency struct {
	Tag              string   `yaml:"tag"`
	Versions         []string `yaml:"versions"`
	AllowPrereleases bool     `yaml:"prereleases"`

	Parameters map[string]string `yaml:"parameters,omitempty"`
}

func (d *Dependency) Validate() error {
	if d.Tag == "" {
		return errors.New("dependency tag is required")
	}

	if strings.Contains(d.Tag, ":") && len(d.Versions) > 0 {
		return errors.New("dependency tag can only specify REGISTRY/NAME when version ranges are specified")
	}

	return nil
}

type CustomActionDefinition struct {
	Description       string `yaml:"description,omitempty"`
	ModifiesResources bool   `yaml:"modifies,omitempty"`
	Stateless         bool   `yaml:"stateless,omitempty"`
}

// OutputDefinition defines a single output for a CNAB
type OutputDefinition struct {
	Name      string   `yaml:"name"`
	ApplyTo   []string `yaml:"applyTo,omitempty"`
	Sensitive bool     `yaml:"sensitive"`

	definition.Schema `yaml:",inline"`
}

func (od *OutputDefinition) Validate() error {
	if od.Name == "" {
		return errors.New("output name is required")
	}

	// TODO: Validate inline Schema

	return nil
}

type BundleOutput struct {
	Name                string `yaml:"name"`
	Path                string `yaml:"path"`
	EnvironmentVariable string `yaml:"env"`
}

type Steps []*Step

func (s Steps) Validate(m *Manifest) error {
	for _, step := range s {
		if step != nil {
			err := step.Validate(m)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

type Step struct {
	Data map[string]interface{} `yaml:",inline"`
}

func (s *Step) Validate(m *Manifest) error {
	if len(s.Data) == 0 {
		return errors.New("no mixin specified")
	}
	if len(s.Data) > 1 {
		return errors.New("more than one mixin specified")
	}

	mixinDeclared := false
	mixinType := s.GetMixinName()
	for _, mixin := range m.Mixins {
		if mixin == mixinType {
			mixinDeclared = true
			break
		}
	}
	if !mixinDeclared {
		return errors.Errorf("mixin (%s) was not declared", mixinType)
	}

	if _, err := s.GetDescription(); err != nil {
		return err
	}

	return nil
}

// GetDescription returns a description of the step.
// Every step must have this property.
func (s *Step) GetDescription() (string, error) {
	if s.Data == nil {
		return "", errors.New("empty step data")
	}

	mixinName := s.GetMixinName()
	children := s.Data[mixinName]
	d, ok := children.(map[interface{}]interface{})["description"]
	if !ok {
		return "", errors.Errorf("mixin step (%s) missing description", mixinName)
	}
	desc, ok := d.(string)
	if !ok {
		return "", errors.Errorf("invalid description type (%T) for mixin step (%s)", desc, mixinName)
	}

	return desc, nil
}

func (s *Step) GetMixinName() string {
	var mixinName string
	for k := range s.Data {
		mixinName = k
	}
	return mixinName
}

func UnmarshalManifest(manifestData []byte) (*Manifest, error) {
	// Unmarshal the manifest into the normal struct
	manifest := &Manifest{}
	err := yaml.Unmarshal(manifestData, &manifest)
	if err != nil {
		return nil, errors.Wrap(err, "error unmarshaling the typed manifest")
	}

	// Do a second pass to identify custom actions, which don't have yaml tags since they are dynamic
	// 1. Marshal the manifest a second time into a plain map
	// 2. Remove keys for fields that are already mapped with yaml tags
	// 3. Anything left is a custom action

	// Marshal the manifest into an untyped map
	unmappedData := make(map[string]interface{})
	err = yaml.Unmarshal(manifestData, &unmappedData)
	if err != nil {
		return nil, errors.Wrap(err, "error unmarshaling the untyped manifest")
	}

	// Use reflection to figure out which fields are on the manifest and have yaml tags
	objValue := reflect.ValueOf(manifest).Elem()
	knownFields := map[string]reflect.Value{}
	for i := 0; i != objValue.NumField(); i++ {
		tagName := strings.Split(objValue.Type().Field(i).Tag.Get("yaml"), ",")[0]
		knownFields[tagName] = objValue.Field(i)
	}

	// Remove any fields that have yaml tags
	for key := range unmappedData {
		if _, found := knownFields[key]; found {
			delete(unmappedData, key)
		}
	}

	// Marshal the remaining keys in the unmappedData as custom actions and append them to the typed manifest
	manifest.CustomActions = make(map[string]Steps, len(unmappedData))
	for key, chunk := range unmappedData {
		chunkData, err := yaml.Marshal(chunk)
		if err != nil {
			return nil, errors.Wrapf(err, "error remarshaling custom action %s", key)
		}

		steps := Steps{}
		err = yaml.Unmarshal(chunkData, &steps)
		if err != nil {
			return nil, errors.Wrapf(err, "error unmarshaling custom action %s", key)
		}

		manifest.CustomActions[key] = steps
	}

	return manifest, nil
}

func (c *Config) readFromFile(path string) (*Manifest, error) {
	if exists, _ := c.FileSystem.Exists(path); !exists {
		return nil, errors.Errorf("the specified porter configuration file %s does not exist", path)
	}

	data, err := c.FileSystem.ReadFile(path)
	if err != nil {
		return nil, errors.Wrapf(err, "could not read manifest at %q", path)
	}

	m, err := UnmarshalManifest(data)
	if err != nil {
		return nil, err
	}

	return m, nil
}

func (c *Config) readFromURL(path string) (*Manifest, error) {
	resp, err := http.Get(path)
	if err != nil {
		return nil, errors.Wrapf(err, "could not reach url %s", path)
	}

	defer resp.Body.Close()
	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrapf(err, "could not read from url %s", path)
	}

	m, err := UnmarshalManifest(data)
	if err != nil {
		return nil, err
	}

	return m, nil
}

// ReadManifest determines if specified path is a URL or a filepath.
// After reading the data in the path it returns a Manifest and any errors
func (c *Config) ReadManifest(path string) (*Manifest, error) {
	if strings.HasPrefix(path, "http") {
		return c.readFromURL(path)
	}

	return c.readFromFile(path)
}

func (c *Config) LoadManifest() error {
	return c.LoadManifestFrom(Name)
}

func (c *Config) LoadManifestFrom(file string) error {
	m, err := c.ReadManifest(file)
	if err != nil {
		return err
	}

	err = m.Validate()
	if err != nil {
		return err
	}

	c.Manifest = m
	c.ManifestPath = file

	return nil
}
