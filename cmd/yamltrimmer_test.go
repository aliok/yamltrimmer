package main

import (
	"bytes"
	"fmt"
	"gopkg.in/yaml.v3"
	"strings"
	"testing"
)

func Test_filterByRules(t *testing.T) {
	tests := []struct {
		name         string
		rules        string
		inputYAML    string
		expectedYAML string
		expectError  bool
	}{
		{
			name: "simple filtering",
			inputYAML: `
            cache:
              enabled: true
              path: /tmp
            database:
              host: localhost
              port: 5432
            `,
			rules: `
            include:
              - key: cache`,
			expectedYAML: `
            cache:
              enabled: true
              path: /tmp
            `,
			expectError: false,
		},
		{
			name: "nested filtering",
			inputYAML: `
            cache:
              enabled: true
            database:
              host: localhost
              port: 5432
              credentials:
                username: user
                password: pass
            `,
			rules: `
            include:
              - key: database
                include:
                    - key: host
                    - key: credentials
                      include:
                      - key: username    
            `,
			expectedYAML: `
            database:
              host: localhost
              credentials:
                username: user
            `,
			expectError: false,
		},
		{
			name: "no matching keys",
			rules: `
            include:            
              - key: nonexistent
            `,
			inputYAML: `
            cache:
              enabled: true
            database:
              host: localhost
              port: 5432
            `,
			expectedYAML: `{}`,
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var inputNode yaml.Node
			err := yaml.Unmarshal([]byte(unindent(tt.inputYAML)), &inputNode)
			if err != nil {
				t.Fatalf("failed to unmarshal input YAML: %v", err)
			}

			var outputNode yaml.Node
			defer func() {
				if r := recover(); r != nil && tt.expectError {
					// Expected error via log.Fatalf
					return
				} else if r != nil {
					t.Fatalf("unexpected panic: %v", r)
				}
			}()

			config, err := parseRules(unindent(tt.rules))
			if err != nil {
				t.Fatalf("failed to parse rules: %v", err)
			}

			// Call the function under test
			filterByRules(config.Include, inputNode.Content[0], &outputNode)

			// Marshal the output node to YAML for comparison
			var outputBuffer bytes.Buffer
			encoder := yaml.NewEncoder(&outputBuffer)
			encoder.SetIndent(2)
			err = encoder.Encode(&outputNode)
			if err != nil {
				t.Fatalf("failed to marshal output YAML: %v", err)
			}

			// Compare the output
			gotYAML := unindent(outputBuffer.String())
			expectedYAML := unindent(tt.expectedYAML)
			if gotYAML != expectedYAML {
				t.Errorf("unexpected result:\nGot:\n%s\nExpected:\n%s", gotYAML, expectedYAML)
			}
		})
	}
}

func unindent(inputYAML string) string {
	inputYAML = strings.TrimLeft(inputYAML, "\n")

	// replace tabs with spaces
	inputYAML = strings.ReplaceAll(inputYAML, "\t", "    ")

	// get the indent level from the first line
	indent := 0
	for _, c := range inputYAML {
		if c == ' ' {
			indent++
		} else {
			break
		}
	}

	// unindent the input YAML
	lines := strings.Split(inputYAML, "\n")
	for i, line := range lines {
		lines[i] = line[indent:]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func parseRules(rules string) (*Configuration, error) {
	var config Configuration
	decoder := yaml.NewDecoder(strings.NewReader(rules))
	if err := decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("error parsing YAML: %w", err)
	}
	return &config, nil
}
