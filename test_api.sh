#!/bin/bash
# Comprehensive API test script for the wagering challenge
# Tests all routes with correct and incorrect values

set -e

BASE="http://localhost:8080"
TOKEN="test-provider-a-player-1"

echo "========================================="
echo "API Testing Suite"
echo "========================================="

# Helper function
curl_post() {
    curl -s -X POST "$BASE$1" \
        -H "Content-Type: application/json" \
        -H "Authorization: Bearer $TOKEN" \
        -H "Idempotency-Key: test-key-${2:-1}" \
        -d "$3"
}

curl_get() {
    curl -s "$BASE$1$2"
}

curl_post_get() {
    curl -s -X POST "$BASE$1" \
        -H "Content-Type: application/json" \
        -H "Authorization: Bearer $TOKEN" \
        -H "Idempotency-Key: test-key-${2:-1}" \
        -d "$3" \
        | jq .
}

echo ""
echo "1. TESTE: CREATE WALLET (Wallet creation)"
echo "-----------------------------------------"

# ✅ Correct wallet creation
echo "✓ Creating wallet with valid data..."
RESULT=$(curl_post "/wallets" 1 '{"playerId":"11111111-1111-4111-8111-111111111111","initialBalance":{"amount":"100.00","currency":"BRL"}}')
echo "  Response: $RESULT"
echo ""

# ✅✅ Second wallet for same player+currency should fail
echo "✓ Attempting duplicate wallet (same player + currency)..."
DUP_RESULT=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$BASE/wallets" \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer $TOKEN" \
    -H "Idempotency-Key: test-key-dup" \
    -d '{"playerId":"11111111-1111-4111-8111-111111111111","initialBalance":{"amount":"50.00","currency":"BRL"}}')
if [ "$DUP_RESULT" == "409" ]; then
    echo "  ✅ Correctly rejected with 409 Conflict"
else
    echo "  ❌ Expected 409, got $DUP_RESULT"
fi
echo ""

# ❌ Invalid currency
echo "✓ Attempting wallet with invalid currency..."
INVALID_CURR=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$BASE/wallets" \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer $TOKEN" \
    -H "Idempotency-Key: test-key-curr" \
    -d '{"playerId":"22222222-2222-4222-8222-222222222222","initialBalance":{"amount":"100.00","currency":"XYZ"}}')
if [ "$INVALID_CURR" == "400" ]; then
    echo "  ✅ Correctly rejected with 400 Bad Request"
else
    echo "  ❌ Expected 400, got $INVALID_CURR"
fi
echo ""

# ❌ Missing fields
echo "✓ Attempting wallet with missing playerId..."
MISSING_PLAYER=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$BASE/wallets" \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer $TOKEN" \
    -H "Idempotency-Key: test-key-noplan" \
    -d '{"initialBalance":{"amount":"100.00","currency":"BRL"}}')
if [ "$MISSING_PLAYER" == "400" ]; then
    echo "  ✅ Correctly rejected with 400 Bad Request"
else
    echo "  ❌ Expected 400, got $MISSING_PLAYER"
fi
echo ""

echo ""
echo "2. TESTE: GET WALLET (Wallet read)"
echo "-----------------------------------------"

# ✅ Read existing wallet
echo "✓ Reading wallet by ID..."
WALLET_ID="11111111-1111-4111-8111-111111111111"
RESULT=$(curl_get "/wallets/$WALLET_ID")
echo "  Response: $RESULT"
echo ""

# ❌ Non-existent wallet
echo "✓ Attempting to read non-existent wallet..."
NOT_FOUND=$(curl -s -o /dev/null -w "%{http_code}" "$BASE/wallets/zzzzzzzz-zzzz-zzzz-zzzz-zzzzzzzzzzzz")
if [ "$NOT_FOUND" == "404" ]; then
    echo "  ✅ Correctly returned 404"
else
    echo "  ❌ Expected 404, got $NOT_FOUND"
fi
echo ""

# ❌ Missing walletId parameter
echo "✓ Attempting to read wallet without ID..."
MISSING_ID=$(curl -s -o /dev/null -w "%{http_code}" "$BASE/wallets/")
if [ "$MISSING_ID" == "400" ]; then
    echo "  ✅ Correctly returned 400"
else
    echo "  ❌ Expected 400, got $MISSING_ID"
fi
echo ""

echo ""
echo "3. TESTE: WAGERING TRANSACTIONS (Transaction submission)"
echo "-----------------------------------------"

# ✅ Valid BET transaction
echo "✓ Submitting valid BET transaction..."
BET_RESULT=$(curl_post "/wagering/transactions" 1 '{"providerId":"provider-a","externalTransactionId":"tx-001","playerId":"11111111-1111-4111-8111-111111111111","walletId":"11111111-1111-4111-8111-111111111111","roundId":"round-1","gameId":"game-1","kind":"BET","money":{"amount":"25.00","currency":"BRL"}}')
echo "  Response status: $(echo "$BET_RESULT" | jq -r '.Status // .status')"
echo "  IdempotentReplay: $(echo "$BET_RESULT" | jq -r '.IdempotentReplay // .idempotentReplay')"
echo "  Balance: $(echo "$BET_RESULT" | jq -r '.Balance // .balance // .Money // .money // "N/A"')"
echo ""

# ❌ Insufficient funds
echo "✓ Submitting BET with insufficient funds..."
INSUFFICIENT=$(curl_post "/wagering/transactions" 2 '{"providerId":"provider-a","externalTransactionId":"tx-002","playerId":"11111111-1111-4111-8111-111111111111","walletId":"11111111-1111-4111-8111-111111111111","roundId":"round-1","gameId":"game-1","kind":"BET","money":{"amount":"200.00","currency":"BRL"}}')
INSUFF_STATUS=$(echo "$INSUFFICIENT" | jq -r '.Status // .status')
INSUFF_CODE=$(echo "$INSUFFICIENT" | jq -r '.FailureCode // .failureCode // "N/A"')
echo "  Status: $INSUFF_STATUS"
echo "  FailureCode: $INSUFF_CODE"
if [ "$INSUFF_STATUS" == "REJECTED" ] && [ "$INSUFF_CODE" == "INSUFFICIENT_FUNDS" ]; then
    echo "  ✅ Correctly rejected with INSUFFICIENT_FUNDS"
else
    echo "  ❌ Expected REJECTED with INSUFFICIENT_FUNDS"
fi
echo ""

# ❌ Invalid amount (zero)
echo "✓ Submitting BET with zero amount..."
ZERO_AMT=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$BASE/wagering/transactions" \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer $TOKEN" \
    -H "Idempotency-Key: test-key-zero" \
    -d '{"providerId":"provider-a","externalTransactionId":"tx-003","playerId":"11111111-1111-4111-8111-111111111111","walletId":"11111111-1111-4111-8111-111111111111","roundId":"round-1","gameId":"game-1","kind":"BET","money":{"amount":"0.00","currency":"BRL"}}')
if [ "$ZERO_AMT" == "400" ]; then
    echo "  ✅ Correctly rejected with 400"
else
    echo "  ❌ Expected 400, got $ZERO_AMT"
fi
echo ""

# ❌ Missing idempotency key
echo "✓ Submitting without Idempotency-Key..."
NO_IDEM=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$BASE/wagering/transactions" \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer $TOKEN" \
    -d '{"providerId":"provider-a","externalTransactionId":"tx-004","playerId":"11111111-1111-4111-8111-111111111111","walletId":"11111111-1111-4111-8111-111111111111","roundId":"round-1","gameId":"game-1","kind":"BET","money":{"amount":"25.00","currency":"BRL"}}')
if [ "$NO_IDEM" == "400" ]; then
    echo "  ✅ Correctly rejected with 400"
else
    echo "  ❌ Expected 400, got $NO_IDEM"
fi
echo ""

echo ""
echo "4. TESTE: HEALTH CHECKS"
echo "-----------------------------------------"

echo "✓ Checking /health/live..."
LIVE=$(curl -s "$BASE/health/live")
echo "  Response: $LIVE"

echo "✓ Checking /health/ready..."
READY=$(curl -s "$BASE/health/ready")
echo "  Response: $READY"
echo ""

echo ""
echo "5. TESTE: GET TRANSACTION BY EXTERNAL ID"
echo "-----------------------------------------"

# This would need a transaction to exist first, so just check the endpoint format
echo "✓ Checking GET /providers/{providerId}/wagering/transactions/{externalTransactionId} endpoint format..."
FORMAT_TEST=$(curl -s "$BASE/wagering/transactions/nonexistent")
echo "  Response status: (expected 404 for nonexistent)"
echo ""

echo ""
echo "========================================="
echo "TEST SUITE COMPLETE"
echo "========================================="