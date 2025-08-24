package benchmark

import (
	"fmt"
	"strings"
	"regexp"

	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/mikhae1/kubectl-quackops/pkg/llm"
)

// TestScenario represents a benchmarking test case
type TestScenario struct {
	Name             string             `json:"name"`
	Description      string             `json:"description"`
	Complexity       ScenarioComplexity `json:"complexity"`
	Category         string             `json:"category"`
	Prompt           string             `json:"prompt"`
	ExpectedCommands []string           `json:"expected_commands,omitempty"`
	ExpectedContains []string           `json:"expected_contains,omitempty"`
	Tags             []string           `json:"tags"`
	EstimatedTokens  int                `json:"estimated_tokens"` // Rough estimate for input
	MockedCmdResults []config.CmdRes    `json:"-"`
}

// ScenarioComplexity represents the complexity level of a test scenario
type ScenarioComplexity string

const (
	ComplexitySimple  ScenarioComplexity = "simple"
	ComplexityMedium  ScenarioComplexity = "medium"
	ComplexityComplex ScenarioComplexity = "complex"
)

// GetDefaultScenarios returns a comprehensive set of kubectl-focused test scenarios
func GetDefaultScenarios() []TestScenario {
	return GetScenariosWithOptions(false)
}

// GetScenariosWithOptions returns scenarios with optional kubectl command generation tests
func GetScenariosWithOptions(includeKubectlGeneration bool) []TestScenario {
	scenarios := []TestScenario{}

	// Add simple scenarios
	scenarios = append(scenarios, getSimpleScenarios()...)

	// Add medium complexity scenarios
	scenarios = append(scenarios, getMediumScenarios()...)

	// Add complex scenarios
	scenarios = append(scenarios, getComplexScenarios()...)

	// Add edge case scenarios
	scenarios = append(scenarios, getEdgeCaseScenarios()...)

	// Optionally add kubectl command generation tests
	if includeKubectlGeneration {
		scenarios = append(scenarios, GetKubectlCommandGenerationScenarios()...)
	}

	return scenarios
}

// getSimpleScenarios returns basic kubectl operation scenarios
func getSimpleScenarios() []TestScenario {
	return []TestScenario{
		{
			Name:        "list_pods",
			Description: "Basic request to list all pods",
			Complexity:  ComplexitySimple,
			Category:    "basic_operations",
			Prompt:      "List all pods and summarize key details",
			ExpectedCommands: []string{
				"kubectl get pods",
				"kubectl get pods -A",
				"kubectl get pods --all-namespaces",
			},
			ExpectedContains: []string{
				"nginx-7b9cd4c798-abcde",
				"redis-0",
				"coredns-5d78c9869d-xyz12",
				"Running",
				"default",
				"kube-system",
			},
			Tags:            []string{"pods", "basic", "list"},
			EstimatedTokens: 15,
			MockedCmdResults: []config.CmdRes{
				{
					Cmd: "kubectl get pods -A -o json",
					Out: `{
  "apiVersion": "v1",
  "items": [
    {
      "metadata": {
        "name": "nginx-7b9cd4c798-abcde",
        "namespace": "default",
        "labels": {"app": "nginx", "version": "1.21"},
        "creationTimestamp": "2024-08-20T10:30:00Z"
      },
      "spec": {
        "containers": [{"name": "nginx", "image": "nginx:1.21"}],
        "nodeName": "node-1"
      },
      "status": {
        "phase": "Running",
        "conditions": [{"type": "Ready", "status": "True"}],
        "containerStatuses": [{"ready": true, "restartCount": 0}]
      }
    },
    {
      "metadata": {
        "name": "redis-0",
        "namespace": "default",
        "labels": {"app": "redis"},
        "creationTimestamp": "2024-08-20T08:15:00Z"
      },
      "spec": {
        "containers": [{"name": "redis", "image": "redis:6.2"}],
        "nodeName": "node-2"
      },
      "status": {
        "phase": "Running",
        "conditions": [{"type": "Ready", "status": "True"}],
        "containerStatuses": [{"ready": true, "restartCount": 1}]
      }
    },
    {
      "metadata": {
        "name": "coredns-5d78c9869d-xyz12",
        "namespace": "kube-system",
        "labels": {"k8s-app": "kube-dns"},
        "creationTimestamp": "2024-08-19T14:22:00Z"
      },
      "spec": {
        "containers": [{"name": "coredns", "image": "coredns/coredns:1.11.1"}],
        "nodeName": "node-1"
      },
      "status": {
        "phase": "Running",
        "conditions": [{"type": "Ready", "status": "True"}],
        "containerStatuses": [{"ready": true, "restartCount": 0}]
      }
    }
  ],
  "kind": "PodList"
}`,
				},
				{
					Cmd: "kubectl get services -A -o json",
					Out: `{
  "apiVersion": "v1",
  "items": [
    {
      "metadata": {"name": "kubernetes", "namespace": "default"},
      "spec": {"type": "ClusterIP", "ports": [{"port": 443, "targetPort": 6443}]}
    },
    {
      "metadata": {"name": "nginx-service", "namespace": "default"},
      "spec": {"type": "ClusterIP", "ports": [{"port": 80, "targetPort": 80}]}
    }
  ],
  "kind": "ServiceList"
}`,
				},
			},
		},
		{
			Name:        "check_pod_status",
			Description: "Check the status of pods",
			Complexity:  ComplexitySimple,
			Category:    "basic_operations",
			Prompt:      "What is the current status of my pods?",
			ExpectedCommands: []string{
				"kubectl get pods",
				"kubectl get pods -o wide",
			},
			ExpectedContains: []string{
				"nginx-7b9cd4c798-abcde",
				"redis-0",
				"Running",
				"node-1",
				"node-2",
			},
			Tags:            []string{"pods", "status", "basic"},
			EstimatedTokens: 18,
			MockedCmdResults: []config.CmdRes{
				{
					Cmd: "kubectl get pods -A -o json",
					Out: `{
  "apiVersion": "v1",
  "items": [
    {
      "metadata": {"name": "nginx-7b9cd4c798-abcde", "namespace": "default"},
      "spec": {"nodeName": "node-1"},
      "status": {
        "phase": "Running",
        "conditions": [{"type": "Ready", "status": "True"}],
        "podIP": "10.0.0.10"
      }
    },
    {
      "metadata": {"name": "redis-0", "namespace": "default"},
      "spec": {"nodeName": "node-2"},
      "status": {
        "phase": "Running",
        "conditions": [{"type": "Ready", "status": "True"}],
        "podIP": "10.0.0.11"
      }
    }
  ],
  "kind": "PodList"
}`,
				},
			},
		},
		{
			Name:        "list_services",
			Description: "Basic request to list services",
			Complexity:  ComplexitySimple,
			Category:    "basic_operations",
			Prompt:      "List all services in the cluster",
			ExpectedCommands: []string{
				"kubectl get services",
				"kubectl get svc",
				"kubectl get svc -A",
			},
			ExpectedContains: []string{
				"my-nginx-ingress-hello-world",
				"redis-master",
			},
			Tags:            []string{"services", "basic", "list"},
			EstimatedTokens: 16,
			MockedCmdResults: []config.CmdRes{
				{
					Cmd: "kubectl get services -A -o json",
					Out: `{
    "apiVersion": "v1",
    "items": [
        {
            "apiVersion": "v1",
            "kind": "Service",
            "metadata": {
                "creationTimestamp": "2023-10-24T15:09:39Z",
                "labels": {
                    "component": "apiserver",
                    "provider": "kubernetes"
                },
                "name": "redis-master",
                "namespace": "default",
                "resourceVersion": "191",
                "uid": "1eef9361-c1ca-408f-987a-aaf03e7fd653"
            },
            "spec": {
                "clusterIP": "10.43.0.1",
                "clusterIPs": [
                    "10.43.0.1"
                ],
                "internalTrafficPolicy": "Cluster",
                "ipFamilies": [
                    "IPv4"
                ],
                "ipFamilyPolicy": "SingleStack",
                "ports": [
                    {
                        "name": "https",
                        "port": 443,
                        "protocol": "TCP",
                        "targetPort": 6443
                    }
                ],
                "sessionAffinity": "None",
                "type": "ClusterIP"
            },
            "status": {
                "loadBalancer": {}
            }
        },
        {
            "apiVersion": "v1",
            "kind": "Service",
            "metadata": {
                "annotations": {
                    "meta.helm.sh/release-name": "my-nginx-ingress",
                    "meta.helm.sh/release-namespace": "default"
                },
                "creationTimestamp": "2024-04-18T07:54:59Z",
                "labels": {
                    "app.kubernetes.io/instance": "my-nginx-ingress",
                    "app.kubernetes.io/managed-by": "Helm",
                    "app.kubernetes.io/name": "hello-world",
                    "app.kubernetes.io/version": "1.16.0",
                    "helm.sh/chart": "hello-world-0.1.0"
                },
                "name": "my-nginx-ingress-hello-world",
                "namespace": "default",
                "resourceVersion": "122645",
                "uid": "f6e6b4d9-d530-4a2c-bbb7-4a6bd3de0d5f"
            },
            "spec": {
                "clusterIP": "10.43.123.91",
                "clusterIPs": [
                    "10.43.123.91"
                ],
                "internalTrafficPolicy": "Cluster",
                "ipFamilies": [
                    "IPv4"
                ],
                "ipFamilyPolicy": "SingleStack",
                "ports": [
                    {
                        "name": "http",
                        "port": 80,
                        "protocol": "TCP",
                        "targetPort": "http"
                    }
                ],
                "selector": {
                    "app.kubernetes.io/instance": "my-nginx-ingress",
                    "app.kubernetes.io/name": "hello-world"
                },
                "sessionAffinity": "None",
                "type": "ClusterIP"
            },
            "status": {
                "loadBalancer": {}
            }
        }
    ],
    "kind": "List",
    "metadata": {
        "resourceVersion": ""
    }
}`,
				},
			},
		},
		{
			Name:        "show_nodes",
			Description: "Display cluster nodes",
			Complexity:  ComplexitySimple,
			Category:    "basic_operations",
			Prompt:      "Show me the nodes in my cluster",
			ExpectedCommands: []string{
				"kubectl get nodes",
				"kubectl get nodes -o wide",
			},
			ExpectedContains: []string{
				"node-1",
				"node-2",
				"Ready",
				"control-plane",
				"worker",
			},
			Tags:            []string{"nodes", "basic", "cluster"},
			EstimatedTokens: 17,
			MockedCmdResults: []config.CmdRes{
				{
					Cmd: "kubectl get nodes -o json",
					Out: `{
  "apiVersion": "v1",
  "items": [
    {
      "metadata": {
        "name": "node-1",
        "labels": {
          "kubernetes.io/hostname": "node-1",
          "node-role.kubernetes.io/control-plane": ""
        }
      },
      "status": {
        "conditions": [{"type": "Ready", "status": "True"}],
        "addresses": [{"type": "InternalIP", "address": "192.168.0.10"}],
        "nodeInfo": {"kubeletVersion": "v1.29.0"}
      }
    },
    {
      "metadata": {
        "name": "node-2",
        "labels": {
          "kubernetes.io/hostname": "node-2",
          "node-role.kubernetes.io/worker": ""
        }
      },
      "status": {
        "conditions": [{"type": "Ready", "status": "True"}],
        "addresses": [{"type": "InternalIP", "address": "192.168.0.11"}],
        "nodeInfo": {"kubeletVersion": "v1.29.0"}
      }
    }
  ],
  "kind": "NodeList"
}`,
				},
			},
		},
	}
}

// getMediumScenarios returns intermediate complexity troubleshooting scenarios
func getMediumScenarios() []TestScenario {
	return []TestScenario{
		{
			Name:        "pod_troubleshooting",
			Description: "Help troubleshoot a failing pod",
			Complexity:  ComplexityMedium,
			Category:    "troubleshooting",
			Prompt:      "My pod is in CrashLoopBackOff state. How can I debug this issue?",
			ExpectedCommands: []string{
				"kubectl describe pod",
				"kubectl logs",
				"kubectl get events",
			},
			ExpectedContains: []string{
				"failing-app-5f8b9c7d6e-xyz99",
				"CrashLoopBackOff",
				"Back-off restarting failed container",
				"connection refused",
			},
			Tags:            []string{"pods", "troubleshooting", "debugging", "crashloop"},
			EstimatedTokens: 25,
			MockedCmdResults: []config.CmdRes{
				{
					Cmd: "kubectl get pods -A -o json",
					Out: `{
  "apiVersion": "v1",
  "items": [
    {
      "metadata": {"name": "failing-app-5f8b9c7d6e-xyz99", "namespace": "default"},
      "status": {
        "phase": "Running",
        "conditions": [{"type": "Ready", "status": "False"}],
        "containerStatuses": [{
          "name": "app",
          "ready": false,
          "restartCount": 5,
          "state": {"waiting": {"reason": "CrashLoopBackOff", "message": "Back-off 2m40s restarting failed container=app pod=failing-app-5f8b9c7d6e-xyz99_default(abc-123)"}},
          "lastState": {"terminated": {"exitCode": 1, "reason": "Error", "message": "connection refused"}}
        }]
      }
    }
  ],
  "kind": "PodList"
}`,
				},
				{
					Cmd: "kubectl get events -A --sort-by=.metadata.creationTimestamp -o json",
					Out: `{
  "apiVersion": "v1",
  "items": [
    {
      "metadata": {"name": "failing-app.17a1b2c3d4e5f6", "namespace": "default"},
      "type": "Warning",
      "reason": "Failed",
      "message": "Back-off restarting failed container app in pod failing-app-5f8b9c7d6e-xyz99_default(abc-123)",
      "involvedObject": {"name": "failing-app-5f8b9c7d6e-xyz99", "kind": "Pod"},
      "firstTimestamp": "2024-08-24T10:15:00Z",
      "lastTimestamp": "2024-08-24T10:25:00Z",
      "count": 10
    },
    {
      "metadata": {"name": "failing-app.17a1b2c3d4e5f7", "namespace": "default"},
      "type": "Warning",
      "reason": "BackOff",
      "message": "Back-off 2m40s restarting failed container=app pod=failing-app-5f8b9c7d6e-xyz99_default(abc-123)",
      "involvedObject": {"name": "failing-app-5f8b9c7d6e-xyz99", "kind": "Pod"},
      "firstTimestamp": "2024-08-24T10:20:00Z",
      "lastTimestamp": "2024-08-24T10:25:00Z",
      "count": 3
    }
  ],
  "kind": "EventList"
}`,
				},
			},
		},
		{
			Name:        "service_connectivity",
			Description: "Debug service connectivity issues",
			Complexity:  ComplexityMedium,
			Category:    "troubleshooting",
			Prompt:      "I can't reach my service. Help me debug the connectivity issue.",
			ExpectedCommands: []string{
				"kubectl get svc",
				"kubectl describe svc",
				"kubectl get endpoints",
				"kubectl get pods -l",
			},
			ExpectedContains: []string{
				"web-service",
				"default",
				"ClusterIP",
				"8080",
			},
			Tags:            []string{"services", "troubleshooting", "networking", "connectivity"},
			EstimatedTokens: 30,
			MockedCmdResults: []config.CmdRes{
				{
					Cmd: "kubectl get services -A -o json",
					Out: `{
  "apiVersion": "v1",
  "items": [
    {
      "metadata": {"name": "web-service", "namespace": "default"},
      "spec": {
        "type": "ClusterIP",
        "clusterIP": "10.96.2.200",
        "ports": [{"port": 8080, "targetPort": 8080, "protocol": "TCP"}],
        "selector": {"app": "web"}
      }
    }
  ],
  "kind": "ServiceList"
}`,
				},
				{
					Cmd: "kubectl get endpoints -A -o json",
					Out: `{
  "apiVersion": "v1",
  "items": [
    {
      "metadata": {"name": "web-service", "namespace": "default"},
      "subsets": []
    }
  ],
  "kind": "EndpointsList"
}`,
				},
				{
					Cmd: "kubectl get pods -A -o json",
					Out: `{
  "apiVersion": "v1",
  "items": [
    {
      "metadata": {
        "name": "web-app-6d7f8g9h0i-abc123",
        "namespace": "default",
        "labels": {"app": "webapp", "version": "v1.0"}
      },
      "status": {"phase": "Running"}
    }
  ],
  "kind": "PodList"
}`,
				},
			},
		},
	}
}

// getComplexScenarios returns complex multi-step analysis scenarios
func getComplexScenarios() []TestScenario {
	return []TestScenario{
		{
			Name:        "cluster_health_check",
			Description: "Comprehensive cluster health analysis",
			Complexity:  ComplexityComplex,
			Category:    "analysis",
			Prompt:      "Perform a comprehensive health check of my Kubernetes cluster and identify any issues.",
			ExpectedCommands: []string{
				"kubectl get nodes",
				"kubectl get pods --all-namespaces",
				"kubectl get events --sort-by=.metadata.creationTimestamp",
				"kubectl top nodes",
				"kubectl top pods",
				"kubectl get componentstatuses",
			},
			ExpectedContains: []string{
				"node-1",
				"node-2",
				"node-3",
				"NotReady",
				"DiskPressure",
				"memory pressure",
				"evicted-pod",
				"Evicted",
				"insufficient resources",
			},
			Tags:            []string{"health", "cluster", "comprehensive", "analysis"},
			EstimatedTokens: 45,
			MockedCmdResults: []config.CmdRes{
				{
					Cmd: "kubectl get nodes -o json",
					Out: `{
  "apiVersion": "v1",
  "items": [
    {
      "metadata": {"name": "node-1", "labels": {"node-role.kubernetes.io/control-plane": ""}},
      "status": {
        "conditions": [
          {"type": "Ready", "status": "True"},
          {"type": "DiskPressure", "status": "False"},
          {"type": "MemoryPressure", "status": "False"}
        ]
      }
    },
    {
      "metadata": {"name": "node-2", "labels": {"node-role.kubernetes.io/worker": ""}},
      "status": {
        "conditions": [
          {"type": "Ready", "status": "True"},
          {"type": "DiskPressure", "status": "True", "message": "disk usage above threshold"},
          {"type": "MemoryPressure", "status": "False"}
        ]
      }
    },
    {
      "metadata": {"name": "node-3", "labels": {"node-role.kubernetes.io/worker": ""}},
      "status": {
        "conditions": [
          {"type": "Ready", "status": "False", "reason": "KubeletNotReady", "message": "container runtime network not ready"},
          {"type": "DiskPressure", "status": "False"},
          {"type": "MemoryPressure", "status": "True", "message": "memory pressure detected"}
        ]
      }
    }
  ],
  "kind": "NodeList"
}`,
				},
				{
					Cmd: "kubectl get pods -A -o json",
					Out: `{
  "apiVersion": "v1",
  "items": [
    {
      "metadata": {"name": "evicted-pod-xyz", "namespace": "default"},
      "status": {
        "phase": "Failed",
        "reason": "Evicted",
        "message": "Pod was evicted due to insufficient resources"
      }
    },
    {
      "metadata": {"name": "healthy-pod-abc", "namespace": "default"},
      "status": {"phase": "Running"}
    }
  ],
  "kind": "PodList"
}`,
				},
				{
					Cmd: "kubectl get events -A --sort-by=.metadata.creationTimestamp -o json",
					Out: `{
  "apiVersion": "v1",
  "items": [
    {
      "metadata": {"name": "node-3.warn1", "namespace": ""},
      "type": "Warning",
      "reason": "NodeNotReady",
      "message": "Node node-3 status is now: NodeNotReady",
      "involvedObject": {"name": "node-3", "kind": "Node"},
      "firstTimestamp": "2024-08-24T09:30:00Z"
    },
    {
      "metadata": {"name": "evicted-pod.warn1", "namespace": "default"},
      "type": "Warning",
      "reason": "Evicted",
      "message": "Pod was evicted due to insufficient resources on node node-2",
      "involvedObject": {"name": "evicted-pod-xyz", "kind": "Pod"},
      "firstTimestamp": "2024-08-24T10:00:00Z"
    }
  ],
  "kind": "EventList"
}`,
				},
			},
		},
	}
}

// getEdgeCaseScenarios returns edge case and stress test scenarios
func getEdgeCaseScenarios() []TestScenario {
	return []TestScenario{
		{
			Name:        "ambiguous_query",
			Description: "Test with an ambiguous or unclear query",
			Complexity:  ComplexityMedium,
			Category:    "edge_cases",
			Prompt:      "Something is broken. Fix it.",
			ExpectedCommands: []string{
				"kubectl get pods --all-namespaces",
				"kubectl get events",
				"kubectl get nodes",
			},
			ExpectedContains: []string{
				"ImagePullBackOff",
				"broken-deployment-abc123",
				"default",
				"ImagePullBackOff",
				"invalid-repo/broken-app:latest",
			},
			Tags:            []string{"ambiguous", "unclear", "general"},
			EstimatedTokens: 8,
			MockedCmdResults: []config.CmdRes{
				{
					Cmd: "kubectl get pods -A -o json",
					Out: `{
  "apiVersion": "v1",
  "items": [
    {
      "metadata": {"name": "broken-deployment-abc123", "namespace": "default"},
      "status": {
        "phase": "Pending",
        "containerStatuses": [{
          "name": "app",
          "ready": false,
          "state": {"waiting": {"reason": "ImagePullBackOff", "message": "Back-off pulling image invalid-repo/broken-app:latest"}},
          "image": "invalid-repo/broken-app:latest"
        }]
      }
    }
  ],
  "kind": "PodList"
}`,
				},
				{
					Cmd: "kubectl get events -A --sort-by=.metadata.creationTimestamp -o json",
					Out: `{
  "apiVersion": "v1",
  "items": [
    {
      "metadata": {"name": "broken-deployment.warn1", "namespace": "default"},
      "type": "Warning",
      "reason": "Failed",
      "message": "Failed to pull image 'invalid-repo/broken-app:latest': rpc error: code = NotFound desc = failed to pull and unpack image",
      "involvedObject": {"name": "broken-deployment-abc123", "kind": "Pod"},
      "firstTimestamp": "2024-08-24T10:10:00Z"
    },
    {
      "metadata": {"name": "broken-deployment.warn2", "namespace": "default"},
      "type": "Warning",
      "reason": "BackOff",
      "message": "Back-off pulling image invalid-repo/broken-app:latest",
      "involvedObject": {"name": "broken-deployment-abc123", "kind": "Pod"},
      "firstTimestamp": "2024-08-24T10:15:00Z"
    }
  ],
  "kind": "EventList"
}`,
				},
				{
					Cmd: "kubectl get nodes -o json",
					Out: `{
  "apiVersion": "v1",
  "items": [
    {
      "metadata": {"name": "node-1"},
      "status": {"conditions": [{"type": "Ready", "status": "True"}]}
    },
    {
      "metadata": {"name": "node-2"},
      "status": {"conditions": [{"type": "Ready", "status": "True"}]}
    }
  ],
  "kind": "NodeList"
}`,
				},
			},
		},
	}
}

// GetScenariosByComplexity returns scenarios filtered by complexity level
func GetScenariosByComplexity(complexity ScenarioComplexity) []TestScenario {
	allScenarios := GetDefaultScenarios()
	filtered := []TestScenario{}

	for _, scenario := range allScenarios {
		if scenario.Complexity == complexity {
			filtered = append(filtered, scenario)
		}
	}

	return filtered
}

// GetScenariosByCategory returns scenarios filtered by category
func GetScenariosByCategory(category string) []TestScenario {
	allScenarios := GetDefaultScenarios()
	filtered := []TestScenario{}

	for _, scenario := range allScenarios {
		if scenario.Category == category {
			filtered = append(filtered, scenario)
		}
	}

	return filtered
}

// GetScenariosByTag returns scenarios that contain the specified tag
func GetScenariosByTag(tag string) []TestScenario {
	allScenarios := GetDefaultScenarios()
	filtered := []TestScenario{}

	for _, scenario := range allScenarios {
		for _, scenarioTag := range scenario.Tags {
			if strings.EqualFold(scenarioTag, tag) {
				filtered = append(filtered, scenario)
				break
			}
		}
	}

	return filtered
}

// GetScenarioByName returns a specific scenario by name
func GetScenarioByName(name string) (*TestScenario, error) {
	allScenarios := GetDefaultScenarios()

	for _, scenario := range allScenarios {
		if scenario.Name == name {
			return &scenario, nil
		}
	}

	return nil, fmt.Errorf("scenario '%s' not found", name)
}

// CreateCustomScenario creates a custom test scenario
func CreateCustomScenario(name, description, prompt string, complexity ScenarioComplexity, category string, expectedCommands []string, tags []string) TestScenario {
	return TestScenario{
		Name:             name,
		Description:      description,
		Complexity:       complexity,
		Category:         category,
		Prompt:           prompt,
		ExpectedCommands: expectedCommands,
		Tags:             tags,
		EstimatedTokens:  len(strings.Fields(prompt)) * 2, // Rough estimate
	}
}

// ValidateScenario checks if a scenario is properly configured
func ValidateScenario(scenario TestScenario) error {
	if scenario.Name == "" {
		return fmt.Errorf("scenario name cannot be empty")
	}

	if scenario.Prompt == "" {
		return fmt.Errorf("scenario prompt cannot be empty")
	}

	if scenario.Complexity == "" {
		return fmt.Errorf("scenario complexity must be specified")
	}

	validComplexities := []ScenarioComplexity{ComplexitySimple, ComplexityMedium, ComplexityComplex}
	isValidComplexity := false
	for _, valid := range validComplexities {
		if scenario.Complexity == valid {
			isValidComplexity = true
			break
		}
	}

	if !isValidComplexity {
		return fmt.Errorf("invalid complexity: %s", scenario.Complexity)
	}

	return nil
}

// GetScenarioStats returns statistics about the scenario set
func GetScenarioStats() map[string]interface{} {
	scenarios := GetDefaultScenarios()

	complexityCounts := make(map[ScenarioComplexity]int)
	categoryCounts := make(map[string]int)

	totalTokens := 0

	for _, scenario := range scenarios {
		complexityCounts[scenario.Complexity]++
		categoryCounts[scenario.Category]++
		totalTokens += scenario.EstimatedTokens
	}

	return map[string]interface{}{
		"total_scenarios":         len(scenarios),
		"complexity_counts":       complexityCounts,
		"category_counts":         categoryCounts,
		"total_estimated_tokens":  totalTokens,
		"avg_tokens_per_scenario": totalTokens / len(scenarios),
	}
}

// GetKubectlCommandGenerationScenarios creates kubectl command generation test scenarios
// from existing scenarios that have ExpectedCommands defined
func GetKubectlCommandGenerationScenarios() []TestScenario {
	baseScenarios := GetDefaultScenarios()
	kubectlScenarios := []TestScenario{}

	for _, scenario := range baseScenarios {
		// Only create kubectl command generation tests for scenarios with ExpectedCommands
		if len(scenario.ExpectedCommands) > 0 {
			kubectlScenario := TestScenario{
				Name:             scenario.Name + "_kubectl",
				Description:      "Test kubectl command generation for: " + scenario.Description,
				Complexity:       scenario.Complexity,
				Category:         scenario.Category + "_kubectl",
				Prompt:           scenario.Prompt,
				ExpectedCommands: scenario.ExpectedCommands,
				ExpectedContains: []string{}, // Clear ExpectedContains as we focus on commands
				Tags:             append(scenario.Tags, "kubectl_generation"),
				EstimatedTokens:  scenario.EstimatedTokens,
				MockedCmdResults: []config.CmdRes{}, // Clear mocked results for command generation test
			}
			kubectlScenarios = append(kubectlScenarios, kubectlScenario)
		}
	}

	return kubectlScenarios
}

// TestKubectlCommandGeneration tests kubectl command generation using GenKubectlCmds
// and validates against ExpectedCommands from scenarios
func TestKubectlCommandGeneration(cfg *config.Config, scenario TestScenario) (bool, float64, error) {
	if len(scenario.ExpectedCommands) == 0 {
		return true, 1.0, fmt.Errorf("scenario %s has no ExpectedCommands to test", scenario.Name)
	}

	// Generate kubectl commands using the same prompt template as the main application
	generatedCommands, err := llm.GenKubectlCmds(cfg, scenario.Prompt, 1)
	if err != nil {
		return false, 0.0, fmt.Errorf("failed to generate kubectl commands: %w", err)
	}

	if len(generatedCommands) == 0 {
		return false, 0.0, fmt.Errorf("no kubectl commands were generated")
	}

	// Validate generated commands against expected commands
	accuracy := ValidateGeneratedCommands(scenario.ExpectedCommands, generatedCommands)
	success := accuracy >= 0.5 // Consider successful if at least 50% of expected commands match

	return success, accuracy, nil
}

// ValidateGeneratedCommands compares generated kubectl commands against expected commands
// Reuses the same logic as evaluateCommandAccuracy from metrics.go but with direct command comparison
func ValidateGeneratedCommands(expectedCommands []string, generatedCommands []string) float64 {
	if len(expectedCommands) == 0 {
		return 1.0
	}

	matches := 0
	normalizedGenerated := make([]string, len(generatedCommands))
	for i, cmd := range generatedCommands {
		normalizedGenerated[i] = NormalizeKubectlCommand(cmd)
	}

	for _, expected := range expectedCommands {
		normalizedExpected := NormalizeKubectlCommand(expected)
		
		// Check if any generated command matches or contains the expected command
		for _, generated := range normalizedGenerated {
			if commandMatches(normalizedExpected, generated) {
				matches++
				break
			}
		}
	}

	return float64(matches) / float64(len(expectedCommands))
}

// NormalizeKubectlCommand normalizes kubectl commands for better comparison
// Handles variations like "kubectl get pods" vs "get pods", namespace flags, etc.
func NormalizeKubectlCommand(cmd string) string {
	// Trim whitespace and convert to lowercase
	normalized := strings.TrimSpace(strings.ToLower(cmd))
	
	// Remove "kubectl" prefix if present
	if strings.HasPrefix(normalized, "kubectl ") {
		normalized = strings.TrimSpace(strings.TrimPrefix(normalized, "kubectl "))
	}

	// Normalize common command variations
	normalized = strings.ReplaceAll(normalized, "--all-namespaces", "-a")
	normalized = strings.ReplaceAll(normalized, "--namespace", "-n")
	normalized = strings.ReplaceAll(normalized, "services", "svc")
	
	// Remove extra spaces
	re := regexp.MustCompile(`\s+`)
	normalized = re.ReplaceAllString(normalized, " ")

	return strings.TrimSpace(normalized)
}

// commandMatches checks if two normalized commands match
// Supports partial matching for flexible command comparison
func commandMatches(expected, generated string) bool {
	// Exact match
	if expected == generated {
		return true
	}

	// Check if the expected command is contained within the generated command
	// This handles cases where generated commands have additional flags
	if strings.Contains(generated, expected) {
		return true
	}

	// Check if they start with the same base command (e.g., "get pods" matches "get pods -o wide")
	expectedParts := strings.Fields(expected)
	generatedParts := strings.Fields(generated)
	
	if len(expectedParts) <= len(generatedParts) {
		match := true
		for i, part := range expectedParts {
			if i >= len(generatedParts) || part != generatedParts[i] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}

	return false
}
