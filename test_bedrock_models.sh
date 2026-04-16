#!/bin/bash

# Test different Claude Sonnet 4.6 model identifiers on Bedrock
# Usage: ./test_bedrock_models.sh

MODEL_IDS=(
  "global.anthropic.claude-sonnet-4-6-20250514-v1:0"
  "us.anthropic.claude-sonnet-4-6-20250514-v1:0"
  "eu.anthropic.claude-sonnet-4-6-20250514-v1:0"
  "anthropic.claude-sonnet-4-6-20250514-v1:0"
  "claude-sonnet-4-6-20250514-v1:0"
  "anthropic.claude-sonnet-4-6-v1:0"
  "claude-sonnet-4-6-v1:0"
)

echo "Testing Bedrock model identifiers..."
echo "======================================"

for model_id in "${MODEL_IDS[@]}"; do
  echo ""
  echo "Testing: $model_id"

  # Try to invoke the model with a simple request
  result=$(aws bedrock-runtime invoke-model \
    --model-id "$model_id" \
    --body '{
      "anthropic_version": "bedrock-2023-05-31",
      "max_tokens": 10,
      "messages": [
        {
          "role": "user",
          "content": "Say hi"
        }
      ]
    }' \
    --cli-binary-format raw-in-base64-out \
    /dev/null 2>&1)

  if echo "$result" | grep -q "ValidationException\|not found\|invalid"; then
    echo "  ❌ FAILED: Model identifier invalid"
  elif echo "$result" | grep -q "error\|Error"; then
    echo "  ⚠️  ERROR: $result"
  else
    echo "  ✅ SUCCESS: Model identifier valid"
  fi
done

echo ""
echo "======================================"
echo "Test complete"
