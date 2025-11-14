package main

import (
	"context"
	"fmt"
	"os"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/aws/smithy-go/auth/bearer"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: bedrock_test <api_key> <model_id>")
		os.Exit(1)
	}
	apiKey := os.Args[1]
	modelID := os.Args[2]

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	bearerProvider := bearer.TokenProviderFunc(func(_ context.Context) (bearer.Token, error) {
		return bearer.Token{Value: apiKey}, nil
	})

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion("us-east-1"),
		awsconfig.WithCredentialsProvider(awssdk.AnonymousCredentials{}),
	)
	if err != nil {
		fmt.Printf("ERROR load config: %v\n", err)
		os.Exit(1)
	}

	client := bedrockruntime.NewFromConfig(awsCfg, func(o *bedrockruntime.Options) {
		o.BaseEndpoint = awssdk.String("https://bedrock-runtime.us-east-1.amazonaws.com")
		o.BearerAuthTokenProvider = bearerProvider
		o.AuthSchemePreference = []string{"smithy.api#httpBearerAuth"}
	})

	out, err := client.Converse(ctx, &bedrockruntime.ConverseInput{
		ModelId: awssdk.String(modelID),
		Messages: []brtypes.Message{
			{Role: brtypes.ConversationRoleUser, Content: []brtypes.ContentBlock{
				&brtypes.ContentBlockMemberText{Value: "hi"},
			}},
		},
		InferenceConfig: &brtypes.InferenceConfiguration{
			MaxTokens: awssdk.Int32(5),
		},
	})
	if err != nil {
		fmt.Printf("ERROR: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("SUCCESS stop=%v\n", out.StopReason)
	if msg, ok := out.Output.(*brtypes.ConverseOutputMemberMessage); ok {
		for _, c := range msg.Value.Content {
			if text, ok := c.(*brtypes.ContentBlockMemberText); ok {
				fmt.Printf("Response: %s\n", text.Value)
			}
		}
	}
}
