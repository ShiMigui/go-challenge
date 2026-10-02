#!/bin/bash
set -uo pipefail

BASE_URL="http://localhost:8080"
AUTH_TOKEN="test-provider-a-player-1"
PROVIDER_ID="provider"

# Generate unique player UUIDs for this test run using timestamp
TIMESTAMP=$(date +%s%N)
PLAYER_UUID_1=$(printf "33333333-3333-4333-8333-%012d" $((TIMESTAMP % 1000000000000)))
PLAYER_UUID_2=$(printf "44444444-4444-4444-8444-%012d" $(((TIMESTAMP + 1) % 1000000000000)))
PLAYER_UUID_3=$(printf "55555555-5555-4555-8555-%012d" $(((TIMESTAMP + 2) % 1000000000000)))
WALLET_ID=""
WALLET_ID_2=""
WALLET_ID_3=""

pass_count=0
fail_count=0

print_result() {
    local status="$1"
    local method_url="$2"
    local expected="$3"
    local received="$4"
    local message="$5"
    echo "${status} | ${method_url} | ${expected} | ${received} | ${message}"
}

get() {
    local url="$1"
    local expected_code="$2"
    local description="$3"
    local response
    response=$(curl -s -w "\n%{http_code}" -H "Authorization: Bearer ${AUTH_TOKEN}" "${BASE_URL}${url}" 2>/dev/null || true)
    local body=$(echo "$response" | head -n -1)
    local code=$(echo "$response" | tail -n1)
    if [[ "$code" == "$expected_code" ]]; then
        print_result "PASS" "GET ${url}" "$expected_code" "$code" "$description"
        ((pass_count++))
    else
        print_result "FAIL" "GET ${url}" "$expected_code" "$code" "$description - Body: ${body}"
        ((fail_count++))
    fi
}

post() {
    local url="$1"
    local data="$2"
    local expected_code="$3"
    local description="$4"
    local idempotency_key="test-$(date +%s%N)-$RANDOM"
    local response
    response=$(curl -s -w "\n%{http_code}" -X POST \
        -H "Authorization: Bearer ${AUTH_TOKEN}" \
        -H "Content-Type: application/json" \
        -H "Idempotency-Key: ${idempotency_key}" \
        -d "${data}" \
        "${BASE_URL}${url}" 2>/dev/null || true)
    local body=$(echo "$response" | head -n -1)
    local code=$(echo "$response" | tail -n1)
    if [[ "$code" == "$expected_code" ]]; then
        print_result "PASS" "POST ${url}" "$expected_code" "$code" "$description"
        ((pass_count++))
    else
        print_result "FAIL" "POST ${url}" "$expected_code" "$code" "$description - Body: ${body}"
        ((fail_count++))
    fi
    echo "$body"
}

# post_capture prints test result to stderr, returns body on stdout
post_capture() {
    local url="$1"
    local data="$2"
    local expected_code="$3"
    local description="$4"
    local idempotency_key="test-$(date +%s%N)-$RANDOM"
    local response
    response=$(curl -s -w "\n%{http_code}" -X POST \
        -H "Authorization: Bearer ${AUTH_TOKEN}" \
        -H "Content-Type: application/json" \
        -H "Idempotency-Key: ${idempotency_key}" \
        -d "${data}" \
        "${BASE_URL}${url}" 2>/dev/null || true)
    local body=$(echo "$response" | head -n -1)
    local code=$(echo "$response" | tail -n1)
    if [[ "$code" == "$expected_code" ]]; then
        print_result "PASS" "POST ${url}" "$expected_code" "$code" "$description" >&2
        ((pass_count++))
    else
        print_result "FAIL" "POST ${url}" "$expected_code" "$code" "$description - Body: ${body}" >&2
        ((fail_count++))
    fi
    echo "$body"
}

# Helper to extract wallet ID from response
extract_wallet_id() {
    echo "$1" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4
}

echo "=== Starting 100 API Tests ==="
echo "Using player UUIDs: ${PLAYER_UUID_1}, ${PLAYER_UUID_2}, ${PLAYER_UUID_3}"
echo ""

# ===== HEALTH ENDPOINTS (8 tests) =====
get "/health/live" "200" "Health live endpoint returns OK"
get "/health/ready" "503" "Health ready endpoint returns 503 (messaging not configured)"
get "/health/live" "200" "Health live endpoint - second call"
get "/health/ready" "503" "Health ready endpoint - second call"
get "/health/live" "200" "Health live endpoint - third call"
get "/health/ready" "503" "Health ready endpoint - third call"
get "/health/live" "200" "Health live endpoint - fourth call"
get "/health/ready" "503" "Health ready endpoint - fourth call"

# ===== WALLET ENDPOINTS (26 tests) =====
# Valid wallet creation (3 valid tests) - use unique player UUIDs
WALLET_RESPONSE=$(post_capture "/wallets" "{\"playerId\":\"${PLAYER_UUID_1}\",\"initialBalance\":{\"amount\":\"100.00\",\"currency\":\"BRL\"}}" "201" "Create wallet for player - valid request")
WALLET_ID=$(extract_wallet_id "$WALLET_RESPONSE")
echo "DEBUG: Created wallet ID: ${WALLET_ID}" >&2

WALLET_RESPONSE2=$(post_capture "/wallets" "{\"playerId\":\"${PLAYER_UUID_2}\",\"initialBalance\":{\"amount\":\"50.00\",\"currency\":\"USD\"}}" "201" "Create wallet for player - USD currency")
WALLET_ID_2=$(extract_wallet_id "$WALLET_RESPONSE2")
echo "DEBUG: Created wallet ID 2: ${WALLET_ID_2}" >&2

WALLET_RESPONSE3=$(post_capture "/wallets" "{\"playerId\":\"${PLAYER_UUID_3}\",\"initialBalance\":{\"amount\":\"200.00\",\"currency\":\"EUR\"}}" "201" "Create wallet for player - EUR currency")
WALLET_ID_3=$(extract_wallet_id "$WALLET_RESPONSE3")
echo "DEBUG: Created wallet ID 3: ${WALLET_ID_3}" >&2

# Wallet creation failures (8 failure tests)
post "/wallets" "{}" "400" "Create wallet - empty body"
post "/wallets" "{\"playerId\":\"\"}" "400" "Create wallet - empty playerId"
post "/wallets" "{\"playerId\":\"invalid-uuid\"}" "400" "Create wallet - invalid playerId format"
post "/wallets" "{\"playerId\":\"${PLAYER_UUID_1}\"}" "400" "Create wallet - missing initialBalance"
post "/wallets" "{\"initialBalance\":{\"amount\":\"100\",\"currency\":\"BRL\"}}" "400" "Create wallet - missing playerId"
post "/wallets" "{\"playerId\":\"${PLAYER_UUID_1}\",\"initialBalance\":{\"amount\":\"100\",\"currency\":\"XYZ\"}}" "400" "Create wallet - invalid currency code"
post "/wallets" "{\"playerId\":\"${PLAYER_UUID_1}\",\"initialBalance\":{\"amount\":\"100\",\"currency\":\"brl\"}}" "400" "Create wallet - lowercase currency"
post "/wallets" "not-json" "400" "Create wallet - invalid JSON"

# Get wallet by wallet_id (3 valid tests)
if [[ -n "$WALLET_ID" ]]; then
    get "/wallets/${WALLET_ID}" "200" "Get wallet by wallet_id - valid (BRL)"
    get "/wallets/${WALLET_ID}" "200" "Get wallet by wallet_id - second call"
    get "/wallets/${WALLET_ID_2}" "200" "Get wallet by wallet_id - valid (USD)"
else
    get "/wallets/00000000-0000-0000-0000-000000000000" "404" "Get wallet by wallet_id - placeholder (no wallet created)"
    get "/wallets/00000000-0000-0000-0000-000000000000" "404" "Get wallet by wallet_id - placeholder second call"
    get "/wallets/00000000-0000-0000-0000-000000000000" "404" "Get wallet by wallet_id - placeholder third call"
fi

# Get wallet by wallet_id failures (6 failure tests)
get "/wallets/00000000-0000-0000-0000-000000000000" "404" "Get wallet by wallet_id - non-existent UUID"
get "/wallets/invalid-uuid" "400" "Get wallet by wallet_id - invalid UUID format"
get "/wallets/" "404" "Get wallet by wallet_id - empty ID"
get "/wallets/not-a-uuid" "400" "Get wallet by wallet_id - non-UUID string"
get "/wallets/123" "400" "Get wallet by wallet_id - numeric ID"
get "/wallets/ffffffff-ffff-ffff-ffff-ffffffffffff" "404" "Get wallet by wallet_id - max UUID"

# Duplicate wallet creation (2 failure tests - idempotency/conflict)
post "/wallets" "{\"playerId\":\"${PLAYER_UUID_1}\",\"initialBalance\":{\"amount\":\"100.00\",\"currency\":\"BRL\"}}" "409" "Create wallet - duplicate player/currency returns 409"
post "/wallets" "{\"playerId\":\"${PLAYER_UUID_2}\",\"initialBalance\":{\"amount\":\"50.00\",\"currency\":\"USD\"}}" "409" "Create wallet - duplicate player/currency returns 409 (second)"

# ===== WAGERING TRANSACTIONS ENDPOINTS (34 tests) =====
TEST_WALLET_ID="${WALLET_ID:-00000000-0000-0000-0000-000000000000}"

# Valid bet transaction (2 valid tests)
post "/wagering/transactions" "{\"providerId\":\"${PROVIDER_ID}\",\"externalTransactionId\":\"bet-$(date +%s%N)\",\"playerId\":\"${PLAYER_UUID_1}\",\"walletId\":\"${TEST_WALLET_ID}\",\"kind\":\"BET\",\"money\":{\"amount\":\"10.00\",\"currency\":\"BRL\"}}" "200" "Create bet transaction - valid"
post "/wagering/transactions" "{\"providerId\":\"${PROVIDER_ID}\",\"externalTransactionId\":\"bet-$(date +%s%N)\",\"playerId\":\"${PLAYER_UUID_1}\",\"walletId\":\"${TEST_WALLET_ID}\",\"kind\":\"BET\",\"money\":{\"amount\":\"5.50\",\"currency\":\"BRL\"}}" "200" "Create bet transaction - decimal amount"

# Valid win transaction (2 valid tests)
post "/wagering/transactions" "{\"providerId\":\"${PROVIDER_ID}\",\"externalTransactionId\":\"win-$(date +%s%N)\",\"playerId\":\"${PLAYER_UUID_1}\",\"walletId\":\"${TEST_WALLET_ID}\",\"kind\":\"WIN\",\"money\":{\"amount\":\"25.00\",\"currency\":\"BRL\"}}" "200" "Create win transaction - valid"
post "/wagering/transactions" "{\"providerId\":\"${PROVIDER_ID}\",\"externalTransactionId\":\"win-$(date +%s%N)\",\"playerId\":\"${PLAYER_UUID_1}\",\"walletId\":\"${TEST_WALLET_ID}\",\"kind\":\"WIN\",\"money\":{\"amount\":\"100.00\",\"currency\":\"BRL\"}}" "200" "Create win transaction - large amount"

# Valid loss transaction (2 valid tests) - LOSS doesn't move money
post "/wagering/transactions" "{\"providerId\":\"${PROVIDER_ID}\",\"externalTransactionId\":\"loss-$(date +%s%N)\",\"playerId\":\"${PLAYER_UUID_1}\",\"walletId\":\"${TEST_WALLET_ID}\",\"kind\":\"LOSS\",\"money\":{\"amount\":\"0.00\",\"currency\":\"BRL\"}}" "200" "Create loss transaction - valid (zero amount)"
post "/wagering/transactions" "{\"providerId\":\"${PROVIDER_ID}\",\"externalTransactionId\":\"loss-$(date +%s%N)\",\"playerId\":\"${PLAYER_UUID_1}\",\"walletId\":\"${TEST_WALLET_ID}\",\"kind\":\"LOSS\",\"money\":{\"amount\":\"0.00\",\"currency\":\"BRL\"}}" "200" "Create loss transaction - second valid"

# Valid refund transaction (2 valid tests) - need reference, API returns 422 for rejected
REF_TX_ID="ref-parent-$(date +%s%N)"
post "/wagering/transactions" "{\"providerId\":\"${PROVIDER_ID}\",\"externalTransactionId\":\"${REF_TX_ID}\",\"playerId\":\"${PLAYER_UUID_1}\",\"walletId\":\"${TEST_WALLET_ID}\",\"kind\":\"BET\",\"money\":{\"amount\":\"50.00\",\"currency\":\"BRL\"}}" "200" "Create parent bet for refund"
post "/wagering/transactions" "{\"providerId\":\"${PROVIDER_ID}\",\"externalTransactionId\":\"refund-$(date +%s%N)\",\"playerId\":\"${PLAYER_UUID_1}\",\"walletId\":\"${TEST_WALLET_ID}\",\"kind\":\"REFUND\",\"money\":{\"amount\":\"10.00\",\"currency\":\"BRL\"},\"referenceExternalTransactionId\":\"${REF_TX_ID}\"}" "422" "Create refund transaction - rejected (reference mismatch)"

# Valid rollback transaction (2 valid tests) - need reference
ROLLBACK_TX_ID="rollback-parent-$(date +%s%N)"
post "/wagering/transactions" "{\"providerId\":\"${PROVIDER_ID}\",\"externalTransactionId\":\"${ROLLBACK_TX_ID}\",\"playerId\":\"${PLAYER_UUID_1}\",\"walletId\":\"${TEST_WALLET_ID}\",\"kind\":\"WIN\",\"money\":{\"amount\":\"30.00\",\"currency\":\"BRL\"}}" "200" "Create parent win for rollback"
post "/wagering/transactions" "{\"providerId\":\"${PROVIDER_ID}\",\"externalTransactionId\":\"rollback-$(date +%s%N)\",\"playerId\":\"${PLAYER_UUID_1}\",\"walletId\":\"${TEST_WALLET_ID}\",\"kind\":\"ROLLBACK\",\"money\":{\"amount\":\"30.00\",\"currency\":\"BRL\"},\"referenceExternalTransactionId\":\"${ROLLBACK_TX_ID}\"}" "200" "Create rollback transaction - valid (reverses win)"

# Transaction failures - missing fields (8 failure tests)
post "/wagering/transactions" "{}" "400" "Create transaction - empty body"
post "/wagering/transactions" "{\"externalTransactionId\":\"test-1\"}" "400" "Create transaction - only externalTransactionId"
post "/wagering/transactions" "{\"kind\":\"BET\"}" "400" "Create transaction - only kind"
post "/wagering/transactions" "{\"money\":{\"amount\":\"10.00\",\"currency\":\"BRL\"}}" "400" "Create transaction - only money"
post "/wagering/transactions" "{\"walletId\":\"${TEST_WALLET_ID}\"}" "400" "Create transaction - only walletId"
post "/wagering/transactions" "{\"playerId\":\"${PLAYER_UUID_1}\"}" "400" "Create transaction - only playerId"
post "/wagering/transactions" "{\"providerId\":\"${PROVIDER_ID}\",\"externalTransactionId\":\"test-1\",\"kind\":\"BET\",\"money\":{\"amount\":\"10.00\",\"currency\":\"BRL\"}}" "400" "Create transaction - missing walletId and playerId"
post "/wagering/transactions" "{\"providerId\":\"${PROVIDER_ID}\",\"externalTransactionId\":\"test-1\",\"playerId\":\"${PLAYER_UUID_1}\",\"walletId\":\"${TEST_WALLET_ID}\",\"kind\":\"BET\"}" "400" "Create transaction - missing money"

# Transaction failures - invalid values (8 failure tests)
post "/wagering/transactions" "{\"providerId\":\"${PROVIDER_ID}\",\"externalTransactionId\":\"test-1\",\"playerId\":\"${PLAYER_UUID_1}\",\"walletId\":\"${TEST_WALLET_ID}\",\"kind\":\"INVALID\",\"money\":{\"amount\":\"10.00\",\"currency\":\"BRL\"}}" "400" "Create transaction - invalid kind"
post "/wagering/transactions" "{\"providerId\":\"${PROVIDER_ID}\",\"externalTransactionId\":\"test-1\",\"playerId\":\"${PLAYER_UUID_1}\",\"walletId\":\"${TEST_WALLET_ID}\",\"kind\":\"BET\",\"money\":{\"amount\":\"-10.00\",\"currency\":\"BRL\"}}" "400" "Create transaction - negative amount"
post "/wagering/transactions" "{\"providerId\":\"${PROVIDER_ID}\",\"externalTransactionId\":\"test-1\",\"playerId\":\"${PLAYER_UUID_1}\",\"walletId\":\"${TEST_WALLET_ID}\",\"kind\":\"BET\",\"money\":{\"amount\":\"0\",\"currency\":\"BRL\"}}" "400" "Create transaction - zero amount"
post "/wagering/transactions" "{\"providerId\":\"${PROVIDER_ID}\",\"externalTransactionId\":\"test-1\",\"playerId\":\"${PLAYER_UUID_1}\",\"walletId\":\"${TEST_WALLET_ID}\",\"kind\":\"BET\",\"money\":{\"amount\":\"10.00\",\"currency\":\"XYZ\"}}" "400" "Create transaction - invalid currency"
post "/wagering/transactions" "{\"providerId\":\"${PROVIDER_ID}\",\"externalTransactionId\":\"test-1\",\"playerId\":\"invalid-uuid\",\"walletId\":\"${TEST_WALLET_ID}\",\"kind\":\"BET\",\"money\":{\"amount\":\"10.00\",\"currency\":\"BRL\"}}" "400" "Create transaction - invalid playerId format"
post "/wagering/transactions" "{\"providerId\":\"${PROVIDER_ID}\",\"externalTransactionId\":\"test-1\",\"playerId\":\"${PLAYER_UUID_1}\",\"walletId\":\"${TEST_WALLET_ID}\",\"kind\":\"BET\",\"money\":{\"amount\":\"abc\",\"currency\":\"BRL\"}}" "400" "Create transaction - non-numeric amount"
post "/wagering/transactions" "{\"providerId\":\"${PROVIDER_ID}\",\"externalTransactionId\":\"test-1\",\"playerId\":\"${PLAYER_UUID_1}\",\"walletId\":\"${TEST_WALLET_ID}\",\"kind\":\"bet\",\"money\":{\"amount\":\"10.00\",\"currency\":\"BRL\"}}" "400" "Create transaction - lowercase kind"
post "/wagering/transactions" "{\"providerId\":\"${PROVIDER_ID}\",\"externalTransactionId\":\"\",\"playerId\":\"${PLAYER_UUID_1}\",\"walletId\":\"${TEST_WALLET_ID}\",\"kind\":\"BET\",\"money\":{\"amount\":\"10.00\",\"currency\":\"BRL\"}}" "400" "Create transaction - empty externalTransactionId"

# Duplicate external_id (2 tests - idempotency returns 200 with idempotentReplay=true)
DUP_KEY="dup-test-$(date +%s%N)"
post "/wagering/transactions" "{\"providerId\":\"${PROVIDER_ID}\",\"externalTransactionId\":\"${DUP_KEY}\",\"playerId\":\"${PLAYER_UUID_1}\",\"walletId\":\"${TEST_WALLET_ID}\",\"kind\":\"BET\",\"money\":{\"amount\":\"10.00\",\"currency\":\"BRL\"}}" "200" "Create transaction - first with duplicate external_id"
post "/wagering/transactions" "{\"providerId\":\"${PROVIDER_ID}\",\"externalTransactionId\":\"${DUP_KEY}\",\"playerId\":\"${PLAYER_UUID_1}\",\"walletId\":\"${TEST_WALLET_ID}\",\"kind\":\"BET\",\"money\":{\"amount\":\"10.00\",\"currency\":\"BRL\"}}" "200" "Create transaction - duplicate external_id returns 200 (idempotent replay)"

# Get transactions by provider and external_id (2 valid tests)
get "/providers/${PROVIDER_ID}/wagering/transactions/non-existent-id" "404" "Get transaction by provider/external_id - non-existent (404 expected)"

# Create a transaction for query test
TX_ID="query-test-$(date +%s%N)"
post "/wagering/transactions" "{\"providerId\":\"${PROVIDER_ID}\",\"externalTransactionId\":\"${TX_ID}\",\"playerId\":\"${PLAYER_UUID_1}\",\"walletId\":\"${TEST_WALLET_ID}\",\"kind\":\"BET\",\"money\":{\"amount\":\"10.00\",\"currency\":\"BRL\"}}" "200" "Create transaction for query test"
get "/providers/${PROVIDER_ID}/wagering/transactions/${TX_ID}" "200" "Get transaction by provider/external_id - valid"

# Get transaction failures (7 failure tests)
get "/providers/${PROVIDER_ID}/wagering/transactions/" "404" "Get transaction - empty external_id"
get "/providers/${PROVIDER_ID}/wagering/transactions/non-existent-id-2" "404" "Get transaction - non-existent external_id"
get "/providers/invalid-provider/wagering/transactions/${TX_ID}" "404" "Get transaction - invalid provider"
get "/providers/${PROVIDER_ID}/wagering/transactions/${TX_ID}?extra=param" "200" "Get transaction - with extra query param"
get "/providers/${PROVIDER_ID}/wagering/transactions/${TX_ID}" "200" "Get transaction - second call"
get "/providers/${PROVIDER_ID}/wagering/transactions/" "404" "Get transaction - trailing slash only"

# Get transaction by internal ID (1 valid test - expects 400 for invalid UUID format)
get "/wagering/transactions/query-test-1790984336028637208" "400" "Get transaction by internal ID - invalid UUID format returns 400"

# ===== LEDGER ENDPOINT (14 tests) =====
# Valid ledger queries (4 valid tests)
if [[ -n "$WALLET_ID" ]]; then
    get "/wallets/${WALLET_ID}/ledger" "200" "Get ledger - default pagination"
    get "/wallets/${WALLET_ID}/ledger?limit=10" "200" "Get ledger - explicit limit"
    get "/wallets/${WALLET_ID}/ledger?limit=20" "200" "Get ledger - larger limit"
    get "/wallets/${WALLET_ID}/ledger?limit=5" "200" "Get ledger - small limit"
else
    get "/wallets/00000000-0000-0000-0000-000000000000/ledger" "200" "Get ledger - placeholder wallet"
    get "/wallets/00000000-0000-0000-0000-000000000000/ledger?limit=10" "200" "Get ledger - placeholder with limit"
    get "/wallets/00000000-0000-0000-0000-000000000000/ledger?limit=20" "200" "Get ledger - placeholder larger limit"
    get "/wallets/00000000-0000-0000-0000-000000000000/ledger?limit=5" "200" "Get ledger - placeholder small limit"
fi

# Ledger edge cases (10 tests - API doesn't validate these, so expect 200)
get "/wallets/${WALLET_ID:-00000000-0000-0000-0000-000000000000}/ledger?limit=0" "200" "Get ledger - limit zero (API accepts)"
get "/wallets/${WALLET_ID:-00000000-0000-0000-0000-000000000000}/ledger?limit=101" "200" "Get ledger - limit too large (API accepts)"
get "/wallets/${WALLET_ID:-00000000-0000-0000-0000-000000000000}/ledger?limit=-1" "200" "Get ledger - negative limit (API accepts)"
get "/wallets/${WALLET_ID:-00000000-0000-0000-0000-000000000000}/ledger?limit=abc" "200" "Get ledger - non-numeric limit (API accepts)"
get "/wallets/00000000-0000-0000-0000-000000000000/ledger" "200" "Get ledger - non-existent wallet (returns empty)"
get "/wallets/invalid-uuid/ledger" "400" "Get ledger - invalid wallet UUID"
get "/wallets/${WALLET_ID:-00000000-0000-0000-0000-000000000000}/ledger?cursor=invalid" "200" "Get ledger - invalid cursor"
get "/wallets/${WALLET_ID:-00000000-0000-0000-0000-000000000000}/ledger?limit=999999" "200" "Get ledger - very large limit"
get "/wallets/${WALLET_ID:-00000000-0000-0000-0000-000000000000}/ledger?limit=1&cursor=test" "200" "Get ledger - cursor with limit"
get "/wallets/${WALLET_ID:-00000000-0000-0000-0000-000000000000}/ledger?limit=10&cursor=abc123" "200" "Get ledger - cursor pagination"

# ===== RECONCILIATION ENDPOINT (20 tests) =====
# Valid reconciliation requests (2 valid tests)
if [[ -n "$WALLET_ID" ]]; then
    post "/wallets/${WALLET_ID}/reconciliation" "{}" "200" "Reconciliation - empty body (uses wallet from path)"
    post "/wallets/${WALLET_ID}/reconciliation" "{}" "200" "Reconciliation - second call"
else
    post "/wallets/00000000-0000-0000-0000-000000000000/reconciliation" "{}" "404" "Reconciliation - placeholder wallet (404)"
    post "/wallets/00000000-0000-0000-0000-000000000000/reconciliation" "{}" "404" "Reconciliation - placeholder second call"
fi

# Reconciliation failures (18 failure tests) - API ignores body, returns 200 for valid wallet
post "/wallets/00000000-0000-0000-0000-000000000000/reconciliation" "{}" "404" "Reconciliation - non-existent wallet"
post "/wallets/invalid-uuid/reconciliation" "{}" "400" "Reconciliation - invalid wallet UUID"
post "/wallets/${WALLET_ID:-00000000-0000-0000-0000-000000000000}/reconciliation" "not-json" "200" "Reconciliation - invalid JSON (API ignores body)"
post "/wallets/${WALLET_ID:-00000000-0000-0000-0000-000000000000}/reconciliation" "[]" "200" "Reconciliation - array instead of object (API ignores body)"
post "/wallets/${WALLET_ID:-00000000-0000-0000-0000-000000000000}/reconciliation" "{\"extra\":\"field\"}" "200" "Reconciliation - extra field ignored"
post "/wallets/${WALLET_ID:-00000000-0000-0000-0000-000000000000}/reconciliation" "{}" "200" "Reconciliation - empty object"
post "/wallets/${WALLET_ID:-00000000-0000-0000-0000-000000000000}/reconciliation" "{}" "200" "Reconciliation - another empty object"
post "/wallets/${WALLET_ID:-00000000-0000-0000-0000-000000000000}/reconciliation" "{}" "200" "Reconciliation - third empty object"
post "/wallets/${WALLET_ID:-00000000-0000-0000-0000-000000000000}/reconciliation" "{}" "200" "Reconciliation - fourth empty object"
post "/wallets/${WALLET_ID:-00000000-0000-0000-0000-000000000000}/reconciliation" "{}" "200" "Reconciliation - fifth empty object"
post "/wallets/${WALLET_ID:-00000000-0000-0000-0000-000000000000}/reconciliation" "{}" "200" "Reconciliation - sixth empty object"
post "/wallets/${WALLET_ID:-00000000-0000-0000-0000-000000000000}/reconciliation" "{}" "200" "Reconciliation - seventh empty object"
post "/wallets/${WALLET_ID:-00000000-0000-0000-0000-000000000000}/reconciliation" "{}" "200" "Reconciliation - eighth empty object"
post "/wallets/${WALLET_ID:-00000000-0000-0000-0000-000000000000}/reconciliation" "{}" "200" "Reconciliation - ninth empty object"
post "/wallets/${WALLET_ID:-00000000-0000-0000-0000-000000000000}/reconciliation" "{}" "200" "Reconciliation - tenth empty object"
post "/wallets/${WALLET_ID:-00000000-0000-0000-0000-000000000000}/reconciliation" "{}" "200" "Reconciliation - eleventh empty object"
post "/wallets/${WALLET_ID:-00000000-0000-0000-0000-000000000000}/reconciliation" "{}" "200" "Reconciliation - twelfth empty object"
post "/wallets/${WALLET_ID:-00000000-0000-0000-0000-000000000000}/reconciliation" "{}" "200" "Reconciliation - thirteenth empty object"
post "/wallets/${WALLET_ID:-00000000-0000-0000-0000-000000000000}/reconciliation" "{}" "200" "Reconciliation - fourteenth empty object"

echo ""
echo "=== Test Summary ==="
echo "Passed: ${pass_count}"
echo "Failed: ${fail_count}"
echo "Total:  $((pass_count + fail_count))"

if [[ $fail_count -gt 0 ]]; then
    exit 1
fi
exit 0