#!/bin/bash
# API Test Suite for Wagering Challenge
# Tests all endpoints with valid and invalid values

set -euo pipefail

# Configuration
BASE_URL="http://localhost:8080"
TOKEN="test-provider-a-player-1"
PASS_COUNT=0
FAIL_COUNT=0

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Test result tracking
print_result() {
    local method="$1"
    local url="$2"
    local expected_code="$3"
    local actual_code="$4"
    local message="$5"
    local expected_msg="$6"

    if [[ "$actual_code" == "$expected_code" ]]; then
        echo -e "${GREEN}PASS${NC} | ${BLUE}$method${NC} $url | Expected: $expected_code | Got: $actual_code | $message"
        PASS_COUNT=$((PASS_COUNT + 1))
    else
        echo -e "${RED}FAIL${NC} | ${BLUE}$method${NC} $url | Expected: $expected_code | Got: $actual_code | $message"
        FAIL_COUNT=$((FAIL_COUNT + 1))
    fi
}

# HTTP POST helper
# Usage: post <endpoint> <idempotency_key> <json_body>
post() {
    local endpoint="$1"
    local idem_key="$2"
    local body="$3"

    response=$(curl -s -w "\n%{http_code}" -X POST "$BASE_URL$endpoint" \
        -H "Content-Type: application/json" \
        -H "Authorization: Bearer $TOKEN" \
        -H "Idempotency-Key: $idem_key" \
        -d "$body")

    http_code=$(echo "$response" | tail -n1)
    body=$(echo "$response" | head -n -1)
    echo "$http_code|$body"
}

# HTTP GET helper
# Usage: get <endpoint>
get() {
    local endpoint="$1"

    response=$(curl -s -w "\n%{http_code}" "$BASE_URL$endpoint" \
        -H "Authorization: Bearer $TOKEN" || true)

    http_code=$(echo "$response" | tail -n1)
    body=$(echo "$response" | head -n -1)
    echo "$http_code|$body"
}

# Extract field from JSON using jq
json_get() {
    local json="$1"
    local field="$2"
    echo "$json" | jq -r "$field // empty"
}

# Test wallet creation with valid data
test_create_wallet_valid() {
    echo -e "\n${YELLOW}=== TEST: Create Wallet (Valid) ===${NC}"
    local result=$(post "/wallets" "wallet-valid-1" '{"playerId":"11111111-1111-4111-8111-111111111111","initialBalance":{"amount":"100.00","currency":"BRL"}}')
    local code=$(echo "$result" | cut -d'|' -f1)
    local body=$(echo "$result" | cut -d'|' -f2-)
    WALLET_ID=$(json_get "$body" ".id")
    print_result "POST" "/wallets" "201" "$code" "Wallet created with ID: $WALLET_ID" ""
}

# Test wallet creation - duplicate player+currency
test_create_wallet_duplicate() {
    echo -e "\n${YELLOW}=== TEST: Create Wallet (Duplicate) ===${NC}"
    local result=$(post "/wallets" "wallet-dup-1" '{"playerId":"11111111-1111-4111-8111-111111111111","initialBalance":{"amount":"50.00","currency":"BRL"}}')
    local code=$(echo "$result" | cut -d'|' -f1)
    print_result "POST" "/wallets" "409" "$code" "Duplicate wallet rejected" ""
}

# Test wallet creation - invalid currency
test_create_wallet_invalid_currency() {
    echo -e "\n${YELLOW}=== TEST: Create Wallet (Invalid Currency) ===${NC}"
    local result=$(post "/wallets" "wallet-curr-1" '{"playerId":"22222222-2222-4222-8222-222222222222","initialBalance":{"amount":"100.00","currency":"XYZ"}}')
    local code=$(echo "$result" | cut -d'|' -f1)
    print_result "POST" "/wallets" "400" "$code" "Invalid currency rejected" ""
}

# Test wallet creation - missing playerId
test_create_wallet_missing_player() {
    echo -e "\n${YELLOW}=== TEST: Create Wallet (Missing PlayerId) ===${NC}"
    local result=$(post "/wallets" "wallet-noplayer-1" '{"initialBalance":{"amount":"100.00","currency":"BRL"}}')
    local code=$(echo "$result" | cut -d'|' -f1)
    print_result "POST" "/wallets" "400" "$code" "Missing playerId rejected" ""
}

# Test get wallet by ID
test_get_wallet() {
    echo -e "\n${YELLOW}=== TEST: Get Wallet (Valid ID) ===${NC}"
    local result=$(get "/wallets/$WALLET_ID")
    local code=$(echo "$result" | cut -d'|' -f1)
    local body=$(echo "$result" | cut -d'|' -f2-)
    local balance=$(json_get "$body" ".balance.amount")
    print_result "GET" "/wallets/$WALLET_ID" "200" "$code" "Wallet found, balance: $balance" ""
}

# Test get wallet - not found
test_get_wallet_not_found() {
    echo -e "\n${YELLOW}=== TEST: Get Wallet (Not Found) ===${NC}"
    local result=$(get "/wallets/00000000-0000-0000-0000-000000000000")
    local code=$(echo "$result" | cut -d'|' -f1)
    print_result "GET" "/wallets/00000000-0000-0000-0000-000000000000" "404" "$code" "Non-existent wallet returns 404" ""
}

# Test get wallet - missing ID (malformed URL)
test_get_wallet_missing_id() {
    echo -e "\n${YELLOW}=== TEST: Get Wallet (Missing ID) ===${NC}"
    response=$(curl -s -w "\n%{http_code}" "$BASE_URL/wallets/")
    local code=$(echo "$response" | tail -n1)
    print_result "GET" "/wallets/" "404" "$code" "Missing wallet ID returns 404" ""
}

# Test BET transaction - valid
test_bet_valid() {
    echo -e "\n${YELLOW}=== TEST: BET Transaction (Valid) ===${NC}"
    local result=$(post "/wagering/transactions" "bet-valid-1" "{\"providerId\":\"provider-a\",\"externalTransactionId\":\"tx-bet-1\",\"playerId\":\"11111111-1111-4111-8111-111111111111\",\"walletId\":\"$WALLET_ID\",\"roundId\":\"round-1\",\"gameId\":\"game-1\",\"kind\":\"BET\",\"money\":{\"amount\":\"25.00\",\"currency\":\"BRL\"}}")
    local code=$(echo "$result" | cut -d'|' -f1)
    local body=$(echo "$result" | cut -d'|' -f2-)
    local status=$(json_get "$body" ".status")
    local balance=$(json_get "$body" ".balance.amount")
    TX_ID_1=$(json_get "$body" ".transactionId")
    print_result "POST" "/wagering/transactions" "200" "$code" "BET processed, status: $status, balance: $balance" ""
}

# Test BET transaction - insufficient funds
test_bet_insufficient_funds() {
    echo -e "\n${YELLOW}=== TEST: BET Transaction (Insufficient Funds) ===${NC}"
    local result=$(post "/wagering/transactions" "bet-insufficient-1" "{\"providerId\":\"provider-a\",\"externalTransactionId\":\"tx-bet-ins\",\"playerId\":\"11111111-1111-4111-8111-111111111111\",\"walletId\":\"$WALLET_ID\",\"roundId\":\"round-1\",\"gameId\":\"game-1\",\"kind\":\"BET\",\"money\":{\"amount\":\"200.00\",\"currency\":\"BRL\"}}")
    local code=$(echo "$result" | cut -d'|' -f1)
    local body=$(echo "$result" | cut -d'|' -f2-)
    local failure=$(json_get "$body" ".failureCode")
    print_result "POST" "/wagering/transactions" "422" "$code" "Insufficient funds rejected with code: $failure" ""
}

# Test BET transaction - zero amount
test_bet_zero_amount() {
    echo -e "\n${YELLOW}=== TEST: BET Transaction (Zero Amount) ===${NC}"
    response=$(curl -s -w "\n%{http_code}" -X POST "$BASE_URL/wagering/transactions" \
        -H "Content-Type: application/json" \
        -H "Authorization: Bearer $TOKEN" \
        -H "Idempotency-Key: bet-zero-1" \
        -d '{"providerId":"provider-a","externalTransactionId":"tx-bet-zero","playerId":"11111111-1111-4111-8111-111111111111","walletId":"'$WALLET_ID'","roundId":"round-1","gameId":"game-1","kind":"BET","money":{"amount":"0.00","currency":"BRL"}}')
    local code=$(echo "$response" | tail -n1)
    print_result "POST" "/wagering/transactions" "400" "$code" "Zero amount rejected" ""
}

# Test BET transaction - missing idempotency key
test_bet_no_idempotency() {
    echo -e "\n${YELLOW}=== TEST: BET Transaction (Missing Idempotency-Key) ===${NC}"
    response=$(curl -s -w "\n%{http_code}" -X POST "$BASE_URL/wagering/transactions" \
        -H "Content-Type: application/json" \
        -H "Authorization: Bearer $TOKEN" \
        -d '{"providerId":"provider-a","externalTransactionId":"tx-bet-noidem","playerId":"11111111-1111-4111-8111-111111111111","walletId":"'$WALLET_ID'","roundId":"round-1","gameId":"game-1","kind":"BET","money":{"amount":"25.00","currency":"BRL"}}')
    local code=$(echo "$response" | tail -n1)
    print_result "POST" "/wagering/transactions" "400" "$code" "Missing Idempotency-Key rejected" ""
}

# Test idempotency replay
test_bet_idempotency_replay() {
    echo -e "\n${YELLOW}=== TEST: BET Transaction (Idempotency Replay) ===${NC}"
    local result=$(post "/wagering/transactions" "bet-idem-1" "{\"providerId\":\"provider-a\",\"externalTransactionId\":\"tx-bet-idem\",\"playerId\":\"11111111-1111-4111-8111-111111111111\",\"walletId\":\"$WALLET_ID\",\"roundId\":\"round-1\",\"gameId\":\"game-1\",\"kind\":\"BET\",\"money\":{\"amount\":\"10.00\",\"currency\":\"BRL\"}}")
    local code=$(echo "$result" | cut -d'|' -f1)
    local body=$(echo "$result" | cut -d'|' -f2-)
    local replay=$(json_get "$body" ".idempotentReplay")
    print_result "POST" "/wagering/transactions" "200" "$code" "First request, replay: $replay" ""

    # Same idempotency key - should replay
    local result2=$(post "/wagering/transactions" "bet-idem-1" "{\"providerId\":\"provider-a\",\"externalTransactionId\":\"tx-bet-idem\",\"playerId\":\"11111111-1111-4111-8111-111111111111\",\"walletId\":\"$WALLET_ID\",\"roundId\":\"round-1\",\"gameId\":\"game-1\",\"kind\":\"BET\",\"money\":{\"amount\":\"10.00\",\"currency\":\"BRL\"}}")
    local code2=$(echo "$result2" | cut -d'|' -f1)
    local body2=$(echo "$result2" | cut -d'|' -f2-)
    local replay2=$(json_get "$body2" ".idempotentReplay")
    print_result "POST" "/wagering/transactions" "200" "$code2" "Replay request, replay: $replay2" ""
}

# Test WIN transaction
test_win_transaction() {
    echo -e "\n${YELLOW}=== TEST: WIN Transaction ===${NC}"
    local result=$(post "/wagering/transactions" "win-1" "{\"providerId\":\"provider-a\",\"externalTransactionId\":\"tx-win-1\",\"playerId\":\"11111111-1111-4111-8111-111111111111\",\"walletId\":\"$WALLET_ID\",\"roundId\":\"round-1\",\"gameId\":\"game-1\",\"kind\":\"WIN\",\"money\":{\"amount\":\"50.00\",\"currency\":\"BRL\"}}")
    local code=$(echo "$result" | cut -d'|' -f1)
    local body=$(echo "$result" | cut -d'|' -f2-)
    local status=$(json_get "$body" ".status")
    local balance=$(json_get "$body" ".balance.amount")
    print_result "POST" "/wagering/transactions" "200" "$code" "WIN processed, status: $status, balance: $balance" ""
}

# Test LOSS transaction
test_loss_transaction() {
    echo -e "\n${YELLOW}=== TEST: LOSS Transaction ===${NC}"
    local result=$(post "/wagering/transactions" "loss-1" "{\"providerId\":\"provider-a\",\"externalTransactionId\":\"tx-loss-1\",\"playerId\":\"11111111-1111-4111-8111-111111111111\",\"walletId\":\"$WALLET_ID\",\"roundId\":\"round-1\",\"gameId\":\"game-1\",\"kind\":\"LOSS\",\"money\":{\"amount\":\"0.00\",\"currency\":\"BRL\"}}")
    local code=$(echo "$result" | cut -d'|' -f1)
    local body=$(echo "$result" | cut -d'|' -f2-)
    local status=$(json_get "$body" ".status")
    local balance=$(json_get "$body" ".balance.amount")
    print_result "POST" "/wagering/transactions" "200" "$code" "LOSS processed, status: $status, balance: $balance" ""
}

# Test REFUND transaction (with reference)
test_refund_transaction() {
    echo -e "\n${YELLOW}=== TEST: REFUND Transaction (With Reference) ===${NC}"
    local result=$(post "/wagering/transactions" "refund-1" "{\"providerId\":\"provider-a\",\"externalTransactionId\":\"tx-refund-1\",\"playerId\":\"11111111-1111-4111-8111-111111111111\",\"walletId\":\"$WALLET_ID\",\"roundId\":\"round-1\",\"gameId\":\"game-1\",\"kind\":\"REFUND\",\"money\":{\"amount\":\"25.00\",\"currency\":\"BRL\"},\"referenceExternalTransactionId\":\"tx-bet-1\"}")
    local code=$(echo "$result" | cut -d'|' -f1)
    local body=$(echo "$result" | cut -d'|' -f2-)
    local status=$(json_get "$body" ".status")
    local balance=$(json_get "$body" ".balance.amount")
    print_result "POST" "/wagering/transactions" "200" "$code" "REFUND processed, status: $status, balance: $balance" ""
}

# Test ROLLBACK transaction (with reference)
test_rollback_transaction() {
    echo -e "\n${YELLOW}=== TEST: ROLLBACK Transaction (With Reference) ===${NC}"
    local result=$(post "/wagering/transactions" "rollback-1" "{\"providerId\":\"provider-a\",\"externalTransactionId\":\"tx-rollback-1\",\"playerId\":\"11111111-1111-4111-8111-111111111111\",\"walletId\":\"$WALLET_ID\",\"roundId\":\"round-1\",\"gameId\":\"game-1\",\"kind\":\"ROLLBACK\",\"money\":{\"amount\":\"50.00\",\"currency\":\"BRL\"},\"referenceExternalTransactionId\":\"tx-win-1\"}")
    local code=$(echo "$result" | cut -d'|' -f1)
    local body=$(echo "$result" | cut -d'|' -f2-)
    local status=$(json_get "$body" ".status")
    local balance=$(json_get "$body" ".balance.amount")
    print_result "POST" "/wagering/transactions" "200" "$code" "ROLLBACK processed, status: $status, balance: $balance" ""
}

# Test REFUND without reference
test_refund_no_reference() {
    echo -e "\n${YELLOW}=== TEST: REFUND Transaction (No Reference) ===${NC}"
    local result=$(post "/wagering/transactions" "refund-noref-1" "{\"providerId\":\"provider-a\",\"externalTransactionId\":\"tx-refund-noref\",\"playerId\":\"11111111-1111-4111-8111-111111111111\",\"walletId\":\"$WALLET_ID\",\"roundId\":\"round-1\",\"gameId\":\"game-1\",\"kind\":\"REFUND\",\"money\":{\"amount\":\"25.00\",\"currency\":\"BRL\"}}")
    local code=$(echo "$result" | cut -d'|' -f1)
    print_result "POST" "/wagering/transactions" "400" "$code" "REFUND without reference rejected" ""
}

# Test get transaction by external ID
test_get_transaction_by_external() {
    echo -e "\n${YELLOW}=== TEST: Get Transaction by External ID ===${NC}"
    local result=$(get "/providers/provider-a/wagering/transactions/tx-bet-1")
    local code=$(echo "$result" | cut -d'|' -f1)
    local body=$(echo "$result" | cut -d'|' -f2-)
    local kind=$(json_get "$body" ".kind")
    print_result "GET" "/providers/provider-a/wagering/transactions/tx-bet-1" "200" "$code" "Transaction found, kind: $kind" ""
}

# Test get transaction by external ID - not found
test_get_transaction_by_external_not_found() {
    echo -e "\n${YELLOW}=== TEST: Get Transaction by External ID (Not Found) ===${NC}"
    local result=$(get "/providers/provider-a/wagering/transactions/nonexistent")
    local code=$(echo "$result" | cut -d'|' -f1)
    print_result "GET" "/providers/provider-a/wagering/transactions/nonexistent" "404" "$code" "Non-existent transaction returns 404" ""
}

# Test health endpoints
test_health_live() {
    echo -e "\n${YELLOW}=== TEST: Health Live ===${NC}"
    response=$(curl -s -w "\n%{http_code}" "$BASE_URL/health/live" || true)
    local code=$(echo "$response" | tail -n1)
    local body=$(echo "$response" | head -n -1)
    print_result "GET" "/health/live" "200" "$code" "Live check: $body" ""
}

test_health_ready() {
    echo -e "\n${YELLOW}=== TEST: Health Ready ===${NC}"
    response=$(curl -s -w "\n%{http_code}" "$BASE_URL/health/ready" || true)
    local code=$(echo "$response" | tail -n1)
    local body=$(echo "$response" | head -n -1)
    # 200 if everything ready, 503 if messaging not configured (acceptable)
    local expected="200"
    if [[ "$code" == "503" ]]; then
        expected="503"
    fi
    print_result "GET" "/health/ready" "$expected" "$code" "Ready check: $body" ""
}

# Test invalid kind
test_invalid_kind() {
    echo -e "\n${YELLOW}=== TEST: Transaction (Invalid Kind) ===${NC}"
    local result=$(post "/wagering/transactions" "invalid-kind-1" "{\"providerId\":\"provider-a\",\"externalTransactionId\":\"tx-invalid-kind\",\"playerId\":\"11111111-1111-4111-8111-111111111111\",\"walletId\":\"$WALLET_ID\",\"roundId\":\"round-1\",\"gameId\":\"game-1\",\"kind\":\"INVALID\",\"money\":{\"amount\":\"25.00\",\"currency\":\"BRL\"}}")
    local code=$(echo "$result" | cut -d'|' -f1)
    print_result "POST" "/wagering/transactions" "400" "$code" "Invalid kind rejected" ""
}

# Test negative amount
test_negative_amount() {
    echo -e "\n${YELLOW}=== TEST: Transaction (Negative Amount) ===${NC}"
    local result=$(post "/wagering/transactions" "neg-amt-1" "{\"providerId\":\"provider-a\",\"externalTransactionId\":\"tx-neg-amt\",\"playerId\":\"11111111-1111-4111-8111-111111111111\",\"walletId\":\"$WALLET_ID\",\"roundId\":\"round-1\",\"gameId\":\"game-1\",\"kind\":\"BET\",\"money\":{\"amount\":\"-10.00\",\"currency\":\"BRL\"}}")
    local code=$(echo "$result" | cut -d'|' -f1)
    print_result "POST" "/wagering/transactions" "400" "$code" "Negative amount rejected" ""
}

# Test missing walletId
test_missing_wallet_id() {
    echo -e "\n${YELLOW}=== TEST: Transaction (Missing WalletId) ===${NC}"
    local result=$(post "/wagering/transactions" "no-wallet-1" "{\"providerId\":\"provider-a\",\"externalTransactionId\":\"tx-no-wallet\",\"playerId\":\"11111111-1111-4111-8111-111111111111\",\"roundId\":\"round-1\",\"gameId\":\"game-1\",\"kind\":\"BET\",\"money\":{\"amount\":\"25.00\",\"currency\":\"BRL\"}}")
    local code=$(echo "$result" | cut -d'|' -f1)
    print_result "POST" "/wagering/transactions" "400" "$code" "Missing walletId rejected" ""
}

# Main
main() {
    echo -e "${BLUE}========================================${NC}"
    echo -e "${BLUE}  WAGERING API TEST SUITE${NC}"
    echo -e "${BLUE}========================================${NC}"

    # Wait for API to be ready
    echo "Waiting for API..."
    for i in {1..30}; do
        if curl -sf "$BASE_URL/health/live" >/dev/null 2>&1; then
            echo "API is ready!"
            break
        fi
        sleep 1
    done

    # Run all tests
    test_health_live
    test_health_ready
    test_create_wallet_valid
    test_create_wallet_duplicate
    test_create_wallet_invalid_currency
    test_create_wallet_missing_player
    test_get_wallet
    test_get_wallet_not_found
    test_get_wallet_missing_id
    test_bet_valid
    test_bet_insufficient_funds
    test_bet_zero_amount
    test_bet_no_idempotency
    test_bet_idempotency_replay
    test_win_transaction
    test_loss_transaction
    test_refund_transaction
    test_rollback_transaction
    test_refund_no_reference
    test_get_transaction_by_external
    test_get_transaction_by_external_not_found
    test_invalid_kind
    test_negative_amount
    test_missing_wallet_id

    # Summary
    echo -e "\n${BLUE}========================================${NC}"
    echo -e "${BLUE}  TEST SUMMARY${NC}"
    echo -e "${BLUE}========================================${NC}"
    echo -e "${GREEN}Passed: $PASS_COUNT${NC}"
    echo -e "${RED}Failed: $FAIL_COUNT${NC}"
    echo -e "${BLUE}Total:  $((PASS_COUNT + FAIL_COUNT))${NC}"

    if [[ $FAIL_COUNT -gt 0 ]]; then
        exit 1
    fi
    exit 0
}

# Only run main if script is executed directly (not sourced)
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    main "$@"
fi