#!/usr/bin/env bash
# Prova que as invariantes financeiras sao impostas pelo banco, e nao apenas
# pelo codigo da aplicacao. Cada caso roda isolado num banco descartavel e
# espera-se "fail" (a constraint barra) ou "pass" (a operacao e legitima).
#
#   make db-test
#
# Cria e destroi o banco migration_test; nao toca no banco da aplicacao.
set -uo pipefail

DB=migration_test
W=11111111-1111-1111-1111-111111111111
P=aaaaaaaa-0000-0000-0000-000000000001
T=22222222-2222-2222-2222-222222222222
ok=0; bad=0

run() {
  local name="$1" expect="$2" sql="$3" got
  if docker compose exec -T -e PGPASSWORD=wagering postgres psql -U wagering -d "$DB" \
       -q -v ON_ERROR_STOP=1 -c "$sql" >/dev/null 2>&1; then got=pass; else got=fail; fi
  if [ "$got" = "$expect" ]; then
    printf '  OK    %-4s %s\n' "$got" "$name"; ok=$((ok+1))
  else
    printf '  FALHA esperava=%s recebeu=%s  %s\n' "$expect" "$got" "$name"; bad=$((bad+1))
  fi
}

scalar() {
  docker compose exec -T -e PGPASSWORD=wagering postgres psql -U wagering -d "$DB" \
    -tAc "$1" 2>/dev/null | tr -d '[:space:]'
}

check() { # nome esperado query
  local name="$1" want="$2" got
  got="$(scalar "$3")"
  if [ "$got" = "$want" ]; then
    printf '  OK    %-4s %s\n' "val" "$name"; ok=$((ok+1))
  else
    printf '  FALHA esperava=%s recebeu=%s  %s\n' "$want" "$got" "$name"; bad=$((bad+1))
  fi
}

# ---- seed -----------------------------------------------------------------
docker compose exec -T -e PGPASSWORD=wagering postgres psql -U wagering -d "$DB" -q -v ON_ERROR_STOP=1 >/dev/null 2>&1 <<EOF
INSERT INTO wallets (id, player_id, currency, balance) VALUES ('$W','$P','BRL',10000);
INSERT INTO wager_transactions (id, provider_id, external_transaction_id, idempotency_key,
  payload_hash, player_id, wallet_id, round_id, game_id, kind, currency, amount, state, processed_at)
  VALUES ('$T','provider-a','tx-seed','provider-a:tx-seed','h','$P','$W','round-1','g1','BET','BRL',2500,'PROCESSED',now());
INSERT INTO wallet_ledger_entries (wallet_id, transaction_id, direction, amount, currency, balance_before, balance_after)
  VALUES ('$W','$T','DEBIT',2500,'BRL',10000,7500);
INSERT INTO wager_transactions (id, provider_id, external_transaction_id, idempotency_key,
  payload_hash, player_id, wallet_id, round_id, game_id, kind, currency, amount, state, processed_at)
  VALUES ('33333333-3333-3333-3333-333333333333','provider-a','tx-win','provider-a:tx-win','h','$P','$W','round-1','g1','WIN','BRL',500,'PROCESSED',now());
EOF
seeded=$?
if [ $seeded -ne 0 ]; then echo "FALHA no seed"; exit 1; fi
echo "seed ok (wallet, BET processada, lancamento no ledger)"
echo

echo "== wallets =="
run "saldo negativo barrado"             fail "INSERT INTO wallets (player_id,currency,balance) VALUES ('$P','USD',-1)"
run "currency nao ISO barrada"            fail "INSERT INTO wallets (player_id,currency,balance) VALUES ('$P','brl',1)"
run "(player,currency) duplicado barrado" fail "INSERT INTO wallets (player_id,currency,balance) VALUES ('$P','BRL',1)"
run "outra moeda para mesmo jogador"      pass "INSERT INTO wallets (player_id,currency,balance) VALUES ('$P','USD',500)"
run "carteira valida aceita"              pass "INSERT INTO wallets (player_id,currency,balance) VALUES ('$P','EUR',0)"

echo "== version controlada pelo banco =="
WVER="select version from wallets where id='$W'"
WBAL="select balance from wallets where id='$W'"
check "version inicial e 1"                1     "$WVER"
run "update so do saldo"                   pass "UPDATE wallets SET balance=9000 WHERE id='$W'"
check "saldo mudou"                         9000  "$WBAL"
check "version incrementou sozinha"         2     "$WVER"
run "update que nao mexe no saldo"         pass "UPDATE wallets SET currency='BRL' WHERE id='$W'"
check "version intacta sem mudanca de saldo" 2    "$WVER"
run "back tentando forcar a version"       pass "UPDATE wallets SET balance=8000, version=99 WHERE id='$W'"
check "trigger ignorou a version enviada"  3     "$WVER"
check "saldo aplicado mesmo assim"          8000  "$WBAL"
scalar "UPDATE wallets SET balance=7000 WHERE id='$W' AND version=1" >/dev/null
check "concorrencia perdida nao mexe no saldo" 8000 "$WBAL"
scalar "UPDATE wallets SET balance=7000 WHERE id='$W' AND version=3" >/dev/null
check "update com version atual aplica"     7000  "$WBAL"
check "version foi para 4"                  4     "$WVER"

echo "== tipos de operacao =="
run "LOSS com valor != 0 barrado"         fail "INSERT INTO wager_transactions (provider_id,external_transaction_id,idempotency_key,payload_hash,player_id,wallet_id,round_id,game_id,kind,currency,amount) VALUES ('pa','l1','pa:l1','h','$P','$W','r','g','LOSS','BRL',100)"
run "LOSS com zero aceito"                pass "INSERT INTO wager_transactions (provider_id,external_transaction_id,idempotency_key,payload_hash,player_id,wallet_id,round_id,game_id,kind,currency,amount) VALUES ('pb','l2','pb:l2','h','$P','$W','r','g','LOSS','BRL',0)"
run "BET com zero barrado"                fail "INSERT INTO wager_transactions (provider_id,external_transaction_id,idempotency_key,payload_hash,player_id,wallet_id,round_id,game_id,kind,currency,amount) VALUES ('pa','b1','pa:b1','h','$P','$W','r','g','BET','BRL',0)"
run "WIN com valor positivo aceito"       pass "INSERT INTO wager_transactions (provider_id,external_transaction_id,idempotency_key,payload_hash,player_id,wallet_id,round_id,game_id,kind,currency,amount) VALUES ('pa','w1','pa:w1','h','$P','$W','r','g','WIN','BRL',2500)"

echo "== idempotencia =="
run "chave duplicada barrada"             fail "INSERT INTO wager_transactions (provider_id,external_transaction_id,idempotency_key,payload_hash,player_id,wallet_id,round_id,game_id,kind,currency,amount) VALUES ('pa','b2','pa:w1','h','$P','$W','r','g','BET','BRL',100)"
run "id externo duplicado barrado"        fail "INSERT INTO wager_transactions (provider_id,external_transaction_id,idempotency_key,payload_hash,player_id,wallet_id,round_id,game_id,kind,currency,amount) VALUES ('provider-a','tx-seed','pa:outro','h','$P','$W','r','g','BET','BRL',100)"
run "externo sem idempotency_key barrado" fail "INSERT INTO wager_transactions (provider_id,external_transaction_id,idempotency_key,payload_hash,player_id,wallet_id,round_id,game_id,kind,currency,amount) VALUES ('pa','b3',NULL,'h','$P','$W','r','g','BET','BRL',100)"
run "externo sem payload_hash barrado"    fail "INSERT INTO wager_transactions (provider_id,external_transaction_id,idempotency_key,payload_hash,player_id,wallet_id,round_id,game_id,kind,currency,amount) VALUES ('pa','b4','pa:b4',NULL,'$P','$W','r','g','BET','BRL',100)"

echo "== abertura interna (OPENING) =="
run "OPENING com campos externos barrado"   fail "INSERT INTO wager_transactions (provider_id,external_transaction_id,idempotency_key,payload_hash,player_id,wallet_id,kind,currency,amount) VALUES ('pa','o1','pa:o1','h','$P','$W','OPENING','BRL',100)"
run "OPENING legitimo aceito"               pass "INSERT INTO wager_transactions (player_id,wallet_id,kind,currency,amount,state,processed_at) VALUES ('$P','$W','OPENING','BRL',5000,'PROCESSED',now())"
run "OPENING com provider barrado"          fail "INSERT INTO wager_transactions (provider_id,player_id,wallet_id,kind,currency,amount) VALUES ('pa','$P','$W','OPENING','BRL',100)"
run "OPENING com round_id barrado"          fail "INSERT INTO wager_transactions (player_id,wallet_id,round_id,kind,currency,amount) VALUES ('$P','$W','r1','OPENING','BRL',100)"
run "OPENING com game_id barrado"           fail "INSERT INTO wager_transactions (player_id,wallet_id,game_id,kind,currency,amount) VALUES ('$P','$W','g1','OPENING','BRL',100)"
run "OPENING com referencia barrado"        fail "INSERT INTO wager_transactions (player_id,wallet_id,kind,currency,amount,reference_external_transaction_id) VALUES ('$P','$W','OPENING','BRL',100,'tx-seed')"
run "BET sem identidade externa barrado"    fail "INSERT INTO wager_transactions (player_id,wallet_id,kind,currency,amount) VALUES ('$P','$W','BET','BRL',100)"

echo "== referencia =="
run "REFUND sem referencia barrado"       fail "INSERT INTO wager_transactions (provider_id,external_transaction_id,idempotency_key,payload_hash,player_id,wallet_id,round_id,game_id,kind,currency,amount) VALUES ('pa','r1','pa:r1','h','$P','$W','r','g','REFUND','BRL',2500)"
run "ROLLBACK sem referencia barrado"     fail "INSERT INTO wager_transactions (provider_id,external_transaction_id,idempotency_key,payload_hash,player_id,wallet_id,round_id,game_id,kind,currency,amount) VALUES ('pa','k1','pa:k1','h','$P','$W','r','g','ROLLBACK','BRL',2500)"
run "BET com referencia barrado"          fail "INSERT INTO wager_transactions (provider_id,external_transaction_id,idempotency_key,payload_hash,player_id,wallet_id,round_id,game_id,kind,currency,amount,reference_external_transaction_id) VALUES ('pa','b5','pa:b5','h','$P','$W','r','g','BET','BRL',100,'tx-seed')"
run "LOSS com referencia barrado"         fail "INSERT INTO wager_transactions (provider_id,external_transaction_id,idempotency_key,payload_hash,player_id,wallet_id,round_id,game_id,kind,currency,amount,reference_external_transaction_id) VALUES ('pa','l3','pa:l3','h','$P','$W','r','g','LOSS','BRL',0,'tx-seed')"
run "REFUND com referencia aceito"        pass "INSERT INTO wager_transactions (provider_id,external_transaction_id,idempotency_key,payload_hash,player_id,wallet_id,round_id,game_id,kind,currency,amount,reference_external_transaction_id) VALUES ('pa','r2','pa:r2','h','$P','$W','r','g','REFUND','BRL',2500,'tx-seed')"
run "WIN com referencia da rodada aceito" pass "INSERT INTO wager_transactions (provider_id,external_transaction_id,idempotency_key,payload_hash,player_id,wallet_id,round_id,game_id,kind,currency,amount,reference_external_transaction_id) VALUES ('pa','w2','pa:w2','h','$P','$W','round-1','g1','WIN','BRL',5000,'tx-seed')"
run "WIN sem referencia tambem aceito"    pass "INSERT INTO wager_transactions (provider_id,external_transaction_id,idempotency_key,payload_hash,player_id,wallet_id,round_id,game_id,kind,currency,amount) VALUES ('pa','w3','pa:w3','h','$P','$W','round-1','g1','WIN','BRL',5000)"

echo "== maquina de estados =="
run "REJECTED sem processed_at barrado"   fail "INSERT INTO wager_transactions (provider_id,external_transaction_id,idempotency_key,payload_hash,player_id,wallet_id,kind,currency,amount,state) VALUES ('pa','s1','pa:s1','h','$P','$W','BET','BRL',100,'REJECTED')"
run "REJECTED sem failure_code barrado"   fail "INSERT INTO wager_transactions (provider_id,external_transaction_id,idempotency_key,payload_hash,player_id,wallet_id,kind,currency,amount,state,processed_at) VALUES ('pa','s2','pa:s2','h','$P','$W','BET','BRL',100,'REJECTED',now())"
run "REJECTED completo aceito"            pass "INSERT INTO wager_transactions (provider_id,external_transaction_id,idempotency_key,payload_hash,player_id,wallet_id,kind,currency,amount,state,processed_at,failure_code) VALUES ('pa','s3','pa:s3','h','$P','$W','BET','BRL',100,'REJECTED',now(),'INSUFFICIENT_FUNDS')"
run "PROCESSED com failure_code barrado"  fail "INSERT INTO wager_transactions (provider_id,external_transaction_id,idempotency_key,payload_hash,player_id,wallet_id,kind,currency,amount,state,processed_at,failure_code) VALUES ('pa','s4','pa:s4','h','$P','$W','BET','BRL',100,'PROCESSED',now(),'X')"
run "PENDING_REFERENCE com ref interna barrada" fail "INSERT INTO wager_transactions (provider_id,external_transaction_id,idempotency_key,payload_hash,player_id,wallet_id,kind,currency,amount,state,reference_external_transaction_id,reference_transaction_id) VALUES ('pa','s5','pa:s5','h','$P','$W','REFUND','BRL',2500,'PENDING_REFERENCE','tx-seed','$T')"
run "PENDING_REFERENCE legitima aceita"   pass "INSERT INTO wager_transactions (provider_id,external_transaction_id,idempotency_key,payload_hash,player_id,wallet_id,kind,currency,amount,state,reference_external_transaction_id) VALUES ('pa','s6','pa:s6','h','$P','$W','REFUND','BRL',2500,'PENDING_REFERENCE','tx-seed')"
run "reference_attempts negativo barrado" fail "INSERT INTO wager_transactions (provider_id,external_transaction_id,idempotency_key,payload_hash,player_id,wallet_id,kind,currency,amount,reference_attempts) VALUES ('pa','s7','pa:s7','h','$P','$W','BET','BRL',100,-1)"

echo "== reversao unica por referencia =="
run "1o REFUND PROCESSED aceito"          pass "INSERT INTO wager_transactions (provider_id,external_transaction_id,idempotency_key,payload_hash,player_id,wallet_id,kind,currency,amount,state,processed_at,reference_external_transaction_id,reference_transaction_id) VALUES ('pa','v1','pa:v1','h','$P','$W','REFUND','BRL',2500,'PROCESSED',now(),'tx-seed','$T')"
run "2o REFUND PROCESSED barrado"         fail "INSERT INTO wager_transactions (provider_id,external_transaction_id,idempotency_key,payload_hash,player_id,wallet_id,kind,currency,amount,state,processed_at,reference_external_transaction_id,reference_transaction_id) VALUES ('pa','v2','pa:v2','h','$P','$W','REFUND','BRL',2500,'PROCESSED',now(),'tx-seed','$T')"
run "ROLLBACK PROCESSED em outra ref aceito" pass "INSERT INTO wager_transactions (provider_id,external_transaction_id,idempotency_key,payload_hash,player_id,wallet_id,kind,currency,amount,state,processed_at,reference_external_transaction_id,reference_transaction_id) VALUES ('pa','v6','pa:v6','h','$P','$W','ROLLBACK','BRL',500,'PROCESSED',now(),'tx-win','33333333-3333-3333-3333-333333333333')"
run "2o ROLLBACK da mesma ref barrado"    fail "INSERT INTO wager_transactions (provider_id,external_transaction_id,idempotency_key,payload_hash,player_id,wallet_id,kind,currency,amount,state,processed_at,reference_external_transaction_id,reference_transaction_id) VALUES ('pa','v9','pa:v9','h','$P','$W','ROLLBACK','BRL',500,'PROCESSED',now(),'tx-win','33333333-3333-3333-3333-333333333333')"
run "REFUND rejeitado nao bloqueia outro" pass "INSERT INTO wager_transactions (provider_id,external_transaction_id,idempotency_key,payload_hash,player_id,wallet_id,kind,currency,amount,state,processed_at,failure_code,reference_external_transaction_id,reference_transaction_id) VALUES ('pa','v4','pa:v4','h','$P','$W','REFUND','BRL',2500,'REJECTED',now(),'ALREADY_REFUNDED','tx-seed','$T')"
run "REFUND aceito em outra referencia"   pass "INSERT INTO wager_transactions (provider_id,external_transaction_id,idempotency_key,payload_hash,player_id,wallet_id,kind,currency,amount,state,processed_at,reference_external_transaction_id,reference_transaction_id) VALUES ('pa','v8','pa:v8','h','$P','$W','REFUND','BRL',500,'PROCESSED',now(),'tx-win','33333333-3333-3333-3333-333333333333')"

echo "== ledger append-only =="
run "aritmetica incoerente barrada"       fail "INSERT INTO wallet_ledger_entries (wallet_id,transaction_id,direction,amount,currency,balance_before,balance_after) VALUES ('$W',gen_random_uuid(),'CREDIT',100,'BRL',500,400)"
run "DEBIT que deixaria negativo barrado" fail "INSERT INTO wallet_ledger_entries (wallet_id,transaction_id,direction,amount,currency,balance_before,balance_after) VALUES ('$W',gen_random_uuid(),'DEBIT',100,'BRL',50,-50)"
run "amount zero barrado"                 fail "INSERT INTO wallet_ledger_entries (wallet_id,transaction_id,direction,amount,currency,balance_before,balance_after) VALUES ('$W',gen_random_uuid(),'CREDIT',0,'BRL',500,500)"
run "lancamento valido aceito"            pass "INSERT INTO wallet_ledger_entries (wallet_id,transaction_id,direction,amount,currency,balance_before,balance_after) VALUES ('$W','33333333-3333-3333-3333-333333333333','CREDIT',500,'BRL',7500,8000)"
run "2o lancamento da mesma tx barrado"   fail "INSERT INTO wallet_ledger_entries (wallet_id,transaction_id,direction,amount,currency,balance_before,balance_after) VALUES ('$W','$T','CREDIT',2500,'BRL',7500,10000)"
run "UPDATE no ledger barrado"            fail "UPDATE wallet_ledger_entries SET amount = 1 WHERE transaction_id = '$T'"
run "DELETE no ledger barrado"            fail "DELETE FROM wallet_ledger_entries WHERE transaction_id = '$T'"
run "TRUNCATE no ledger barrado"          fail "TRUNCATE wallet_ledger_entries"
got_count="$(docker compose exec -T -e PGPASSWORD=wagering postgres psql -U wagering -d "$DB" -tAc "select count(*) from wallet_ledger_entries where transaction_id='$T'" 2>/dev/null | tr -d '[:space:]')"
if [ "$got_count" = "1" ]; then
  printf '  OK    %-4s %s\n' "pass" "ledger com 1 lancamento para a tx (append-only preservado)"; ok=$((ok+1))
else
  printf '  FALHA esperado=1 recebeu=%s  %s\n' "$got_count" "ledger com 1 lancamento para a tx"; bad=$((bad+1))
fi

echo "== inbox =="
run "primeira mensagem aceita"            pass "INSERT INTO inbox (consumer_name,message_id,message_hash) VALUES ('c1','m1','h')"
run "mensagem duplicada barrada"          fail "INSERT INTO inbox (consumer_name,message_id,message_hash) VALUES ('c1','m1','h')"
run "outro consumidor aceita"             pass "INSERT INTO inbox (consumer_name,message_id,message_hash) VALUES ('c2','m1','h')"
run "completed_at antes de received barrado" fail "INSERT INTO inbox (consumer_name,message_id,message_hash,received_at,completed_at) VALUES ('c3','m3','h',now(),now()-interval '1 hour')"
run "inbox completa aceita"               pass "INSERT INTO inbox (consumer_name,message_id,message_hash,completed_at) VALUES ('c4','m4','h',now())"

echo "== outbox =="
run "payload nao-objeto barrado"          fail "INSERT INTO outbox (aggregate_type,aggregate_id,event_type,event_version,payload) VALUES ('wagering',gen_random_uuid(),'E',1,'\"texto\"')"
run "event_version 0 barrado"             fail "INSERT INTO outbox (aggregate_type,aggregate_id,event_type,event_version,payload) VALUES ('wagering',gen_random_uuid(),'E',0,'{}')"
run "locked_by sem locked_at barrado"     fail "INSERT INTO outbox (aggregate_type,aggregate_id,event_type,event_version,payload,locked_by) VALUES ('wagering',gen_random_uuid(),'E',1,'{}','w1')"
run "lock consistente aceito"             pass "INSERT INTO outbox (aggregate_type,aggregate_id,event_type,event_version,payload,locked_by,locked_at) VALUES ('wagering',gen_random_uuid(),'E',1,'{}','w1',now())"
run "published_at antes de occurred_at barrado" fail "INSERT INTO outbox (aggregate_type,aggregate_id,event_type,event_version,payload,occurred_at,published_at) VALUES ('wagering',gen_random_uuid(),'E',1,'{}',now(),now()-interval '1 hour')"
run "outbox pendente aceita"              pass "INSERT INTO outbox (aggregate_type,aggregate_id,event_type,event_version,payload) VALUES ('wagering',gen_random_uuid(),'E',1,'{}')"

echo
echo "RESULTADO: $ok ok, $bad falhas"
[ "$bad" -eq 0 ] 2>/dev/null || exit 1
exit 0
