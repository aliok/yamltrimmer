package main

import (
	"bytes"
	"crypto/md5"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

type CacheConfig struct {
	Enabled bool   `yaml:"enabled,omitempty"`
	Path    string `yaml:"path,omitempty"`
}

type IncludeConfigItem struct {
	Key     string              `yaml:"key"`
	Include []IncludeConfigItem `yaml:"include,omitempty"`
}

type Configuration struct {
	Input   string              `yaml:"input"`
	Output  string              `yaml:"output"`
	Cache   CacheConfig         `yaml:"cache,omitempty"`
	Include []IncludeConfigItem `yaml:"include"`
}

func parseConfiguration(filePath string) (*Configuration, error) {
	// Open the YAML file
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("error opening file: %w", err)
	}
	defer file.Close()

	// TODO: doesn't handle missing fields and defaults
	// Decode the YAML into the Configuration struct
	var config Configuration
	decoder := yaml.NewDecoder(file)
	if err := decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("error parsing YAML: %w", err)
	}

	return &config, nil
}

// isURL checks if a string is a valid URL
func isURL(str string) bool {
	// Simple check for URL (could be more comprehensive)
	return strings.HasPrefix(str, "http://") || strings.HasPrefix(str, "https://")
}

// isFile checks if a string is a valid file path
func isFile(str string) bool {
	// Check if the input string is a valid file path
	_, err := os.Stat(str)
	return err == nil && !isURL(str)
}

func downloadFile(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("error downloading file: %w", err)
	}
	defer resp.Body.Close()

	// Read the body of the response
	fileData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading file body: %w", err)
	}

	return fileData, nil
}

func checkCacheAndDownload(url, localFilePath, etagFilePath string) error {
	// Read the stored ETag from the file (if it exists)
	var storedEtag string
	if etagFile, err := os.ReadFile(etagFilePath); err == nil {
		storedEtag = string(etagFile)
	}

	// Create a new HTTP request with the stored ETag
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	if storedEtag != "" {
		req.Header.Set("If-None-Match", storedEtag)
	}

	// Make the HTTP request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to make HTTP request: %w", err)
	}
	defer resp.Body.Close()

	// Check the response status
	if resp.StatusCode == http.StatusNotModified {
		logrus.Debug("Resource not modified. Skipping download.")
		return nil
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Get the new ETag from the response headers
	newEtag := resp.Header.Get("ETag")
	if newEtag == "" {
		logrus.Debug("No ETag found in response. Proceeding to download.")
	}

	// Write the content to the local file
	localFile, err := os.Create(localFilePath)
	if err != nil {
		return fmt.Errorf("failed to create local file: %w", err)
	}
	defer localFile.Close()

	if _, err = io.Copy(localFile, resp.Body); err != nil {
		return fmt.Errorf("failed to write content to local file: %w", err)
	}

	logrus.Debug("File downloaded successfully:", localFilePath)

	// Save the new ETag to the ETag file
	if newEtag != "" {
		if err := os.WriteFile(etagFilePath, []byte(newEtag), 0644); err != nil {
			return fmt.Errorf("failed to write ETag to file: %w", err)
		}
		logrus.Debug("ETag updated:", newEtag)
	}

	return nil
}

func generateFileName(url, extension string) string {
	hash := fmt.Sprintf("%x", md5.Sum([]byte(url)))
	if extension == "" {
		return hash
	}
	return fmt.Sprintf("%s.%s", hash, extension)
}

func filterByRules(rules []IncludeConfigItem, inputNode, outputNode *yaml.Node) {
	if inputNode.Kind != yaml.MappingNode {
		logrus.Fatalf("Input node is not a mapping node")
	}

	// Create an output node as a mapping node
	outputNode.Kind = yaml.MappingNode
	outputNode.Style = inputNode.Style

	// Iterate over the rules
	for _, rule := range rules {
		// Find the corresponding key in the input YAML
		for i := 0; i < len(inputNode.Content); i += 2 {
			keyNode := inputNode.Content[i]
			valueNode := inputNode.Content[i+1]

			if keyNode.Value == rule.Key {
				// Add the key to the output
				outputNode.Content = append(outputNode.Content, keyNode)

				// If there are nested rules, process the value node recursively
				if len(rule.Include) > 0 {
					var nestedOutputNode yaml.Node
					filterByRules(rule.Include, valueNode, &nestedOutputNode)
					outputNode.Content = append(outputNode.Content, &nestedOutputNode)
				} else {
					// Otherwise, copy the value node directly
					outputNode.Content = append(outputNode.Content, valueNode)
				}
				break
			}
		}
	}
}

func trim(input []byte, rules []IncludeConfigItem) ([]byte, error) {
	// Parse the input YAML into a yaml.Node
	var root yaml.Node
	if err := yaml.Unmarshal(input, &root); err != nil {
		return nil, fmt.Errorf("failed to unmarshal input YAML: %w", err)
	}
	logrus.Debugf("Parsed input YAML successfully")

	// get the first node
	if len(root.Content) == 0 {
		return nil, fmt.Errorf("no content in the input YAML")
	}

	// TODO: handle multiple documents later
	if len(root.Content) > 1 {
		logrus.Fatalf("Multiple documents in the input YAML. This is not supported yet.")
	}
	root = *root.Content[0]

	// Apply trimming rules recursively
	var outputNode yaml.Node
	filterByRules(rules, &root, &outputNode)
	logrus.Debugf("Trimmed input YAML successfully")

	// Marshal the filtered data back into YAML format
	var output bytes.Buffer
	encoder := yaml.NewEncoder(&output)
	encoder.SetIndent(2)
	if err := encoder.Encode(&outputNode); err != nil {
		return nil, fmt.Errorf("failed to marshal output YAML: %w", err)
	}
	logrus.Debugf("Marshalled output YAML successfully")

	return output.Bytes(), nil
}

func main() {
	// Define a flag for the configuration file path
	configPath := flag.String("config", "config.yaml", "Path to the configuration file")
	verbose := flag.Bool("verbose", false, "Enable verbose logging")
	flag.Parse()

	if *verbose {
		logrus.SetLevel(logrus.DebugLevel)
		logrus.Debug("Verbose logging enabled")
		logrus.Debugf("Configuration file path: %s", *configPath)
	}

	// Resolve the relative path to an absolute path
	absPath, err := filepath.Abs(*configPath)
	if err != nil {
		logrus.Fatalf("Failed to resolve the configuration file path: %v", err)
	}
	logrus.Debugf("Resolved configuration file path: %s", absPath)

	// Call the function to parse the configuration
	config, err := parseConfiguration(absPath)
	if err != nil {
		logrus.Fatalf("Failed to parse configuration: %v", err)
	}
	logrus.Debugf("Parsed configuration: %+v", *config)

	// see if we're using a cache
	if isURL(config.Input) && config.Cache.Enabled {
		logrus.Debugf("Cache enabled with path: %s", config.Cache.Path)
		if config.Cache.Path == "" {
			logrus.Debugf("Cache enabled but no path specified. Going to use the default cache path.")
			homeDir, err := os.UserHomeDir()
			if err != nil {
				logrus.Fatalf("Failed to get user home directory: %v", err)
			}
			config.Cache.Path = filepath.Join(homeDir, ".yamltrimmer-cache")
		}

		// resolve the cache path to an absolute path
		absCachePath, err := filepath.Abs(config.Cache.Path)
		if err != nil {
			logrus.Fatalf("Failed to resolve the cache path: %v", err)
		}
		logrus.Debugf("Resolved cache path: %s", absCachePath)
		config.Cache.Path = absCachePath

		// create the cache directory, if it doesn't exist
		if _, err := os.Stat(config.Cache.Path); os.IsNotExist(err) {
			logrus.Debugf("Creating cache directory: %s", config.Cache.Path)
			err := os.MkdirAll(config.Cache.Path, 0755)
			if err != nil {
				logrus.Fatalf("Failed to create cache directory: %v", err)
			}
		} else if err != nil {
			logrus.Fatalf("Failed to check cache directory: %v", err)
		}
	}

	// resolve the output path to an absolute path
	absOutputPath, err := filepath.Abs(config.Output)
	if err != nil {
		logrus.Fatalf("Failed to resolve the output file path: %v", err)
	}
	logrus.Debugf("Resolved output file path: %s", absOutputPath)
	config.Output = absOutputPath

	content := []byte{}

	if isURL(config.Input) {
		logrus.Debugf("Input is a URL: %s", config.Input)

		if config.Cache.Enabled {
			logrus.Debugf("Going to try to read the input file from cache")

			localFileName := generateFileName(config.Input, "")
			etagFileName := generateFileName(config.Input, "etag")

			localFilePath := filepath.Join(config.Cache.Path, localFileName)
			etagFilePath := filepath.Join(config.Cache.Path, etagFileName)

			logrus.Debugf("Local file path: %s", localFilePath)
			logrus.Debugf("ETag file path: %s", etagFilePath)

			logrus.Debugf("Checking and downloading file: %s", config.Input)
			if err := checkCacheAndDownload(config.Input, localFilePath, etagFilePath); err != nil {
				logrus.Fatalf("Failed to download file: %v", err)
			}

			// Read the input file
			content, err = os.ReadFile(localFilePath)
			if err != nil {
				logrus.Fatalf("Failed to read input file from cache: %v", err)
			}
		} else {
			logrus.Debugf("Going to download the input file")
			if content, err = downloadFile(config.Input); err != nil {
				logrus.Fatalf("Failed to download input file: %v", err)
			}
		}
	} else if isFile(config.Input) {
		logrus.Debugf("Input is a file: %s", config.Input)
		// Read the input file
		if content, err = os.ReadFile(config.Input); err != nil {
			logrus.Fatalf("Failed to read input file: %v", err)
		}
	} else {
		logrus.Fatalf("Invalid input: not a URL or a valid file path")
	}

	logrus.Debugf("Done reading input data: %d bytes", len(content))
	if len(content) == 0 {
		logrus.Fatalf("Input data is empty")
	} else if len(content) < 100 {
		logrus.Debugf("Input data: %s", string(content))
	} else {
		logrus.Debugf("Input data (first 100 bytes): %s", string(content)[:100])
	}

	// Trim the input data
	var trimmedContent []byte
	if trimmedContent, err = trim(content, config.Include); err != nil {
		logrus.Fatalf("Failed to trim input data: %v", err)
	}

	logrus.Debugf("Done trimming input data: %d bytes", len(trimmedContent))
	if len(trimmedContent) == 0 {
		logrus.Fatalf("Trimmed data is empty")
	} else if len(trimmedContent) < 100 {
		logrus.Debugf("Trimmed data: %s", string(trimmedContent))
	} else {
		logrus.Debugf("Trimmed data (first 100 bytes): %s", string(trimmedContent)[:100])
	}

	// Write the trimmed data to the output file
	if err := os.WriteFile(config.Output, trimmedContent, 0644); err != nil {
		logrus.Fatalf("Failed to write output file: %v", err)
	}
	logrus.Debugf("Output file written successfully: %s", config.Output)
}
