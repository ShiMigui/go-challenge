package wagering

import (
	"context"
	"log"
	"time"

	"github.com/shimigui/go-challenge/internal/application/ports"
	"github.com/shimigui/go-challenge/internal/domain/wager"
)

// ReferenceWorker retoma as operações que ficaram esperando referência.
//
// A pendência é durável: a submissão grava PENDING_REFERENCE com prazo e o
// worker assume daí em diante. A retomada refaz o mesmo percurso da
// submissão — resolve a referência quando ela chega, rejeita ao esgotar as
// tentativas — então nenhum comportamento fica de fora ao reiniciar.
type ReferenceWorker struct {
	wagerRepo wager.WagerTransactionRepository
	txManager ports.TransactionManager
	now       func() time.Time
}

// NewReferenceWorker monta o worker com os ports de escrita.
func NewReferenceWorker(
	wagerRepo wager.WagerTransactionRepository,
	txManager ports.TransactionManager,
) *ReferenceWorker {
	return &ReferenceWorker{
		wagerRepo: wagerRepo,
		txManager: txManager,
		now:       time.Now,
	}
}

// SetNow troca o relógio do worker. É porta de teste: o comportamento de
// retomada depende do limite de prazo, e a produção usa o relógio real.
func (w *ReferenceWorker) SetNow(now func() time.Time) { w.now = now }

// Retomar processa o lote de pendências vencidas dentro da transação.
//
// Devolve quantas operações saíram de PENDING_REFERENCE (PROCESSED ou
// REJECTED). Falha de infraestrutura interrompe o lote — a próxima
// varredura reaparece — mas cada operação individual é atômica.
func (w *ReferenceWorker) Retomar(ctx context.Context) (int, error) {
	pendentes, err := w.wagerRepo.ListPendingReferences(ctx, w.now(), limitReferenceBatch)
	if err != nil {
		return 0, err
	}

	resolvidas := 0
	for i := range pendentes {
		pendencia := pendentes[i]
		var final *wager.Transaction
		err := w.txManager.InTransaction(ctx, func(uow ports.UnitOfWork) error {
			t, err := uow.Transactions().FindByID(ctx, pendencia.ID())
			if err != nil {
				return err
			}
			if t.State() != wager.StatePendingReference {
				// Outra instância do worker concluiu enquanto íamos ler.
				return nil
			}
			if err := processa(ctx, uow, t, w.now()); err != nil {
				return err
			}
			final = t
			return nil
		})
		if err != nil {
			return resolvidas, err
		}
		if final != nil && final.State().IsTerminal() {
			resolvidas++
		}
	}
	return resolvidas, nil
}

// Loop varre as pendências vencidas em intervalos regulares até o contexto
// ser cancelado. A primeira varredura corre na subida, para retomar operações
// órfãs de um processo anterior.
func (w *ReferenceWorker) Loop(ctx context.Context) {
	imediato := make(chan struct{}, 1)
	imediato <- struct{}{}

	ticker := time.NewTicker(referencePollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-imediato:
		case <-ticker.C:
		}

		resolvidas, err := w.Retomar(ctx)
		if err != nil && ctx.Err() == nil {
			log.Printf("wagering: varredura de referencias falhou: %v", err)
			continue
		}
		if resolvidas > 0 {
			log.Printf("wagering: %d operacoes retomadas do PENDING_REFERENCE", resolvidas)
		}
	}
}
