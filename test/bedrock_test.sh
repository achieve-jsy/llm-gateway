#!/bin/bash

# 测试 Bedrock 上可用的 Claude 模型 ID

echo "=== 测试 AWS Bedrock Claude 模型 ID ==="
echo ""

# 从数据库获取 AWS 凭证
# 凭证一律从环境变量注入，禁止硬编码（guardrails C1）。
# 用法：PGPASSWORD=*** PGHOST=... PGPORT=5433 PGUSER=llm_gateway PGDATABASE=llm_gateway_prod ./bedrock_test.sh
: "${PGPASSWORD:?请通过环境变量 PGPASSWORD 提供数据库口令}"
CREDS=$(psql -h "${PGHOST:-localhost}" -p "${PGPORT:-5433}" -U "${PGUSER:-llm_gateway}" -d "${PGDATABASE:-llm_gateway_prod}" -t -A -c "
SELECT encrypted_credentials
FROM providers
WHERE name = 'aws'
LIMIT 1;
")

# 解密凭证（假设你有解密脚本或手动解密）
# 这里我们使用已知的 AWS 凭证进行测试
# 注意：实际使用时需要从数据库解密获取

echo "注意：此脚本需要在生产服务器上运行，并使用正确的 AWS 凭证"
echo ""

# 测试的模型 ID 列表
MODEL_IDS=(
    "anthropic.claude-sonnet-4-6-20250514-v1:0"
    "anthropic.claude-sonnet-4-6-20250615-v1:0"
    "anthropic.claude-sonnet-4-6-20250701-v1:0"
    "anthropic.claude-sonnet-4-6-20250714-v1:0"
    "anthropic.claude-sonnet-4-6-v1:0"
    "anthropic.claude-3-5-sonnet-20241022-v2:0"
    "anthropic.claude-3-5-sonnet-20240620-v1:0"
    "anthropic.claude-3-haiku-20240307-v1:0"
    "anthropic.claude-3-opus-20240229-v1:0"
)

echo "测试模型 ID："
echo ""

for MODEL_ID in "${MODEL_IDS[@]}"; do
    echo -n "测试 $MODEL_ID ... "

    # 使用 AWS CLI 测试（如果可用）
    # 或者使用 curl 直接调用 Bedrock API
    # 这里使用占位符，实际需要根据你的 AWS 认证方式实现

    # 示例 curl 命令（需要 AWS Signature V4 签名）
    # 由于签名复杂，建议使用 AWS CLI 或 SDK

    echo "需要 AWS CLI 或 SDK 测试"
done

echo ""
echo "=== 建议 ==="
echo ""
echo "1. 在生产服务器上安装 AWS CLI："
echo "   curl 'https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip' -o 'awscliv2.zip'"
echo "   unzip awscliv2.zip"
echo "   sudo ./aws/install"
echo ""
echo "2. 配置 AWS 凭证："
echo "   aws configure"
echo "   # 输入从数据库解密得到的 Access Key ID 和 Secret Access Key"
echo ""
echo "3. 使用 AWS CLI 列出可用模型："
echo "   aws bedrock list-foundation-models --by-provider Anthropic --query 'modelSummaries[*].modelId'"
echo ""
echo "4. 测试特定模型："
echo "   aws bedrock invoke-model --model-id anthropic.claude-sonnet-4-6-20250514-v1:0 \\"
echo "     --body '{\"anthropic_version\":\"bedrock-2023-05-31\",\"max_tokens\":10,\"messages\":[{\"role\":\"user\",\"content\":\"Hi\"}]}' \\"
echo "     --cli-binary-format raw-in-base64-out /tmp/response.json"
echo ""
echo "5. 或者查看 Bedrock 控制台确认可用模型列表"
