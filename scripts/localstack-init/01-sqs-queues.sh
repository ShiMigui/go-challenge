#!/usr/bin/env bash
# LocalStack SQS initialization - creates FIFO queues with DLQ and redrive policy.
# Runs automatically on LocalStack startup via /etc/localstack/init/ready.d

set -euo pipefail

ENDPOINT="http://localhost:4566"
REGION="${CLOUD_REGION:-us-east-1}"

# Queue names (match .env.example)
MAIN_QUEUE="${SQS_QUEUE_NAME:-wager-transactions.fifo}"
DLQ_NAME="${SQS_QUEUE_DLQ_NAME:-wager-transactions-dlq.fifo}"
EVENTS_QUEUE="${SQS_EVENTS_QUEUE_NAME:-wager-events.fifo}"

# DLQ max receive count before moving to DLQ
MAX_RECEIVE_COUNT="${SQS_MAX_RECEIVE_COUNT:-3}"

echo "[localstack-init] Creating SQS queues..."

# Create DLQ first (no redrive policy)
aws --endpoint-url="${ENDPOINT}" --region="${REGION}" sqs create-queue \
    --queue-name "${DLQ_NAME}" \
    --attributes '{"FifoQueue":"true","ContentBasedDeduplication":"true"}' \
    >/dev/null || echo "[localstack-init] DLQ may already exist"

# Get DLQ ARN for redrive policy
DLQ_ARN=$(aws --endpoint-url="${ENDPOINT}" --region="${REGION}" sqs get-queue-attributes \
    --queue-url "${ENDPOINT}/000000000000/${DLQ_NAME}" \
    --attribute-names QueueArn \
    --query 'Attributes.QueueArn' --output text 2>/dev/null || echo "")

if [[ -n "${DLQ_ARN}" ]]; then
    REDRIVE_POLICY=$(printf '{"deadLetterTargetArn":"%s","maxReceiveCount":%d}' "${DLQ_ARN}" "${MAX_RECEIVE_COUNT}")
else
    REDRIVE_POLICY=""
fi

# Create main FIFO queue with redrive policy to DLQ
aws --endpoint-url="${ENDPOINT}" --region="${REGION}" sqs create-queue \
    --queue-name "${MAIN_QUEUE}" \
    --attributes "$(printf '{"FifoQueue":"true","ContentBasedDeduplication":"true"%s}' "${REDRIVE_POLICY:+,${REDRIVE_POLICY}}")" \
    >/dev/null || echo "[localstack-init] Main queue may already exist"

# Create events FIFO queue (also with DLQ)
aws --endpoint-url="${ENDPOINT}" --region="${REGION}" sqs create-queue \
    --queue-name "${EVENTS_QUEUE}" \
    --attributes "$(printf '{"FifoQueue":"true","ContentBasedDeduplication":"true"%s}' "${REDRIVE_POLICY:+,${REDRIVE_POLICY}}")" \
    >/dev/null || echo "[localstack-init] Events queue may already exist"

echo "[localstack-init] SQS queues ready:"
aws --endpoint-url="${ENDPOINT}" --region="${REGION}" sqs list-queues --output table 2>/dev/null || true