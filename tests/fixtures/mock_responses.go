package fixtures

import (
	"fmt"
	"strings"
	"time"

	"github.com/mikhae1/kubectl-quackops/pkg/llm"
	"github.com/tmc/langchaingo/llms"
)

// MockResponseFixtures provides predefined test data for various scenarios
type MockResponseFixtures struct{}

// Kubernetes-specific responses
func (MockResponseFixtures) KubernetesPodExplanation() []llm.MockResponse {
	return []llm.MockResponse{
		{
			Content: `# Kubernetes Pods

Pods are the smallest deployable units in Kubernetes. Here are the key points:

## What is a Pod?
- A pod is a group of one or more containers
- Containers in a pod share storage and network
- Pods are ephemeral - they can be created, destroyed, and recreated

## Pod Lifecycle
1. **Pending**: Pod has been accepted but containers are not yet running
2. **Running**: Pod has been bound to a node and all containers are created
3. **Succeeded**: All containers have terminated successfully
4. **Failed**: At least one container has failed
5. **Unknown**: Pod state cannot be determined

## Common Commands
- ` + "`kubectl get pods`" + ` - List all pods
- ` + "`kubectl describe pod <name>`" + ` - Get detailed information
- ` + "`kubectl logs <pod-name>`" + ` - View pod logs

Would you like me to help you with any specific pod-related issues?`,
			TokensUsed: 180,
		},
	}
}

func (MockResponseFixtures) KubernetesServiceExplanation() []llm.MockResponse {
	return []llm.MockResponse{
		{
			Content: `# Kubernetes Services

Services provide stable network endpoints for accessing pods:

## Service Types
- **ClusterIP**: Internal cluster access only (default)
- **NodePort**: Exposes service on each node's IP at a static port
- **LoadBalancer**: Exposes service externally using cloud provider's load balancer
- **ExternalName**: Maps service to external DNS name

## How Services Work
- Services use selectors to target pods with specific labels
- They provide load balancing across multiple pod replicas
- Services maintain stable IP addresses even as pods are recreated

## Troubleshooting Services
- Check service endpoints: ` + "`kubectl get endpoints`" + `
- Verify pod labels match service selector
- Test connectivity from within cluster

Let me know if you need help with any service-related issues!`,
			TokensUsed: 165,
		},
	}
}

func (MockResponseFixtures) TroubleshootingWorkflow() []llm.MockResponse {
	return []llm.MockResponse{
		{
			Content: `# Kubernetes Troubleshooting Workflow

I'll help you troubleshoot your Kubernetes issue systematically:

## Step 1: Gather Information
Let me start by checking the basic status of your resources.`,
			TokensUsed: 45,
		},
		{
			Content: `## Step 2: Analyze Pod Status
Based on the pod information, I can see potential issues with container startup. Let me check the events and logs.`,
			TokensUsed: 50,
		},
		{
			Content: `## Step 3: Root Cause Analysis
The issue appears to be related to image pull problems. Here's what we need to fix:

1. **Image Pull Policy**: Check if the image exists and is accessible
2. **Registry Credentials**: Verify image pull secrets are configured
3. **Network Connectivity**: Ensure nodes can reach the container registry

## Recommended Actions:
- Run ` + "`kubectl describe pod <failing-pod>`" + ` to see detailed events
- Check ` + "`kubectl get events --sort-by=.metadata.creationTimestamp`" + `
- Verify image name and tag are correct

Would you like me to help you implement these fixes?`,
			TokensUsed: 120,
		},
	}
}

// Error scenarios
func (MockResponseFixtures) NetworkError() []llm.MockResponse {
	return []llm.MockResponse{
		{
			Error: fmt.Errorf("network error: connection timeout"),
		},
	}
}

func (MockResponseFixtures) RateLimitError() []llm.MockResponse {
	return []llm.MockResponse{
		{
			SimulateRateLimit: true,
		},
	}
}

func (MockResponseFixtures) RetryScenario() []llm.MockResponse {
	return []llm.MockResponse{
		{
			Error: fmt.Errorf("temporary failure - retry recommended"),
		},
		{
			Content:    "Success after retry - the system is now responding normally.",
			TokensUsed: 35,
		},
	}
}

// Streaming responses
func (MockResponseFixtures) StreamingExplanation() []llm.MockResponse {
	return []llm.MockResponse{
		{
			Content: "This is a complete streaming response about Kubernetes networking concepts.",
			StreamingChunks: []string{
				"This is a complete streaming response ",
				"about Kubernetes networking ",
				"concepts. Services provide stable endpoints, ",
				"while ingress controllers handle external traffic routing. ",
				"Network policies control pod-to-pod communication.",
			},
			StreamingDelay: 50 * time.Millisecond,
			TokensUsed:     85,
		},
	}
}

func (MockResponseFixtures) StreamingTroubleshooting() []llm.MockResponse {
	return []llm.MockResponse{
		{
			Content: "Let me analyze your cluster step by step to identify the root cause of this issue.",
			StreamingChunks: []string{
				"Let me analyze your cluster ",
				"step by step ",
				"to identify the root cause ",
				"of this issue. ",
				"\n\nFirst, I'll check the pod status... ",
				"Now examining the events... ",
				"Finally, reviewing the logs for errors.",
			},
			StreamingDelay: 100 * time.Millisecond,
			TokensUsed:     90,
		},
	}
}

// Complex conversation flows
func (MockResponseFixtures) ConversationFlow() []llm.MockResponse {
	return []llm.MockResponse{
		{
			Content:    "Hello! I'm here to help you with your Kubernetes cluster. What seems to be the issue?",
			TokensUsed: 50,
		},
		{
			Content:    "I understand you're having pod startup issues. Let me gather some information to help diagnose the problem.",
			TokensUsed: 55,
		},
		{
			Content:    "Based on your description, this sounds like it could be an image pull issue, resource constraint, or configuration problem. Let me check a few things.",
			TokensUsed: 65,
		},
		{
			Content:    "Perfect! I can see the issue now. The pod is failing because of insufficient memory resources. Here's how to fix it:",
			TokensUsed: 60,
		},
	}
}

// Tool call responses
func (MockResponseFixtures) WithMCPToolCalls() []llm.MockResponse {
	return []llm.MockResponse{
		{
			Content: "I need to check your cluster status to help with this issue.",
			ToolCalls: []llms.ToolCall{
				{
					ID: "tool_call_1",
					FunctionCall: &llms.FunctionCall{
						Name:      "kubectl_get_pods",
						Arguments: `{"namespace": "default"}`,
					},
				},
				{
					ID: "tool_call_2", 
					FunctionCall: &llms.FunctionCall{
						Name:      "kubectl_get_events",
						Arguments: `{"namespace": "default", "sort_by": "lastTimestamp"}`,
					},
				},
			},
			TokensUsed: 70,
		},
		{
			Content: "Based on the tool results, I can see that your pods are in CrashLoopBackOff state due to memory issues. Here's the solution:",
			TokensUsed: 55,
		},
	}
}

// Different provider responses for testing
func (MockResponseFixtures) OpenAIResponse() []llm.MockResponse {
	return []llm.MockResponse{
		{
			Content:    "This is a response formatted in the style typical of OpenAI models, with clear structure and helpful explanations.",
			TokensUsed: 60,
		},
	}
}

func (MockResponseFixtures) AnthropicResponse() []llm.MockResponse {
	return []llm.MockResponse{
		{
			Content:    "I'll help you understand this Kubernetes concept thoroughly. Let me break this down systematically with clear explanations and practical examples.",
			TokensUsed: 65,
		},
	}
}

func (MockResponseFixtures) OllamaResponse() []llm.MockResponse {
	return []llm.MockResponse{
		{
			Content:    "Here's a concise explanation of the Kubernetes concept you asked about, with focus on practical implementation details.",
			TokensUsed: 45,
		},
	}
}

// Performance testing data
func (MockResponseFixtures) LargeResponse() []llm.MockResponse {
	content := `# Comprehensive Kubernetes Guide

` + strings.Repeat("This is a detailed explanation of Kubernetes concepts. ", 50) + `

## Architecture Overview
` + strings.Repeat("Kubernetes follows a master-worker architecture pattern. ", 30) + `

## Best Practices
` + strings.Repeat("Always follow security best practices when deploying applications. ", 25) + `

## Troubleshooting
` + strings.Repeat("When troubleshooting issues, start with checking pod status and events. ", 40)

	return []llm.MockResponse{
		{
			Content:    content,
			TokensUsed: 800,
		},
	}
}

func (MockResponseFixtures) SmallResponse() []llm.MockResponse {
	return []llm.MockResponse{
		{
			Content:    "Brief answer: Yes, that's correct.",
			TokensUsed: 8,
		},
	}
}

// Multi-language responses for internationalization testing
func (MockResponseFixtures) MultiLanguageResponse() []llm.MockResponse {
	return []llm.MockResponse{
		{
			Content: `# Kubernetes Explanation / Explicación de Kubernetes

English: Kubernetes is a container orchestration platform.
Español: Kubernetes es una plataforma de orquestación de contenedores.
Français: Kubernetes est une plateforme d'orchestration de conteneurs.

## Universal Commands / Comandos Universales:
- kubectl get pods
- kubectl describe pod
- kubectl logs`,
			TokensUsed: 95,
		},
	}
}

// Edge cases and error conditions
func (MockResponseFixtures) EmptyResponse() []llm.MockResponse {
	return []llm.MockResponse{
		{
			Content:    "",
			TokensUsed: 0,
		},
	}
}

func (MockResponseFixtures) MalformedJSON() []llm.MockResponse {
	return []llm.MockResponse{
		{
			Content:    `{"incomplete": json response without closing brace`,
			TokensUsed: 25,
		},
	}
}

func (MockResponseFixtures) SpecialCharacters() []llm.MockResponse {
	return []llm.MockResponse{
		{
			Content:    "Response with special chars: 🚀 ⚡ 💻 kubectl get pods | grep -E '(Running|Pending)' && echo 'Status check complete'",
			TokensUsed: 35,
		},
	}
}

// Scenario builders for complex test cases
type ScenarioBuilder struct {
	responses []llm.MockResponse
}

func NewScenarioBuilder() *ScenarioBuilder {
	return &ScenarioBuilder{
		responses: make([]llm.MockResponse, 0),
	}
}

func (sb *ScenarioBuilder) AddResponse(content string, tokens int) *ScenarioBuilder {
	sb.responses = append(sb.responses, llm.MockResponse{
		Content:    content,
		TokensUsed: tokens,
	})
	return sb
}

func (sb *ScenarioBuilder) AddError(err error) *ScenarioBuilder {
	sb.responses = append(sb.responses, llm.MockResponse{
		Error: err,
	})
	return sb
}

func (sb *ScenarioBuilder) AddStreamingResponse(content string, chunks []string, delay time.Duration, tokens int) *ScenarioBuilder {
	sb.responses = append(sb.responses, llm.MockResponse{
		Content:         content,
		StreamingChunks: chunks,
		StreamingDelay:  delay,
		TokensUsed:      tokens,
	})
	return sb
}

func (sb *ScenarioBuilder) AddRateLimit() *ScenarioBuilder {
	sb.responses = append(sb.responses, llm.MockResponse{
		SimulateRateLimit: true,
	})
	return sb
}

func (sb *ScenarioBuilder) Build() []llm.MockResponse {
	return sb.responses
}