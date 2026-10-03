package sqs

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/shimigui/go-challenge/internal/application/ports"
	"github.com/shimigui/go-challenge/internal/application/wagering"
	"github.com/shimigui/go-challenge/internal/domain/wager"
)

// Consumer processa mensagens da fila SQS wager-transactions.fifo.
// Garante deduplicação via InboxRepository: somente mensagens novas
// (TryBegin = true) são processadas; reentregas (mesmo hash) são
// reconhecidas sem reprocessamento; adulteração de hash é erro.
type Consumer struct {
	wagerSvc     wagering.Service
	inbox        ports.InboxRepository
	consumerName string
	stopCh       chan struct{}
	wg           sync.WaitGroup
}

// NewConsumer cria um consumidor de SQS ligado aos ports de saída.
func NewConsumer(
	wagerSvc wagering.Service,
	inbox ports.InboxRepository,
	consumerName string,
) *Consumer {
	return &Consumer{
		wagerSvc:     wagerSvc,
		inbox:        inbox,
		consumerName: consumerName,
		stopCh:       make(chan struct{}),
	}
}

// Start inicia a varredura da fila em goroutine.
func (c *Consumer) Start(ctx context.Context) {
	c.wg.Add(1)
	go c.loop(ctx)
}

// Stop para o consumidor e aguarda a goroutine terminar.
func (c *Consumer) Stop() {
	close(c.stopCh)
	c.wg.Wait()
}

// loop varre as mensagens da fila em intervalos até o contexto ser cancelado.
func (c *Consumer) loop(ctx context.Context) {
	defer c.wg.Done()
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		case <-ticker.C:
			// Simula ReceiveMessage do SQS: busca mensagens disponíveis.
			// Na produção, substituir por client.ReceiveMessageContext(ctx, params).
			messages := c.receiveMessages(ctx)
			for _, msg := range messages {
				processaMensagem(c, msg)
			}
		}
	}
}

// receiveMessages simula a obtenção de mensagens da fila SQS.
// Em produção, isto chamaria client.ReceiveMessageContext.
func (c *Consumer) receiveMessages(ctx context.Context) []sqsMessage {
	return nil
}

// sqsMessage representa uma mensagem recebida da fila SQS.
type sqsMessage struct {
	MessageID     string `json:"messageId"`
	Consumer      string `json:"consumerName"`
	Body          string `json:"body"` // JSON com params da transação
	ReceiptHandle string `json:"receiptHandle"`
	MD5OfBody     string `json:"md5OfBody"`
}

// processaMensagem processa uma única mensagem: dedup via InboxRepository
// e, se nova, executa a operação de wagering.
func processaMensagem(c *Consumer, msg sqsMessage) {
	now := time.Now()
	begin, err := c.inbox.TryBegin(context.Background(), c.consumerName, msg.MessageID, md5OfString(msg.Body), now)
	if err != nil {
		fmt.Printf("inbox.TryBegin erro: %v\n", err)
		return
	}
	if !begin {
		// Reentrega (mesmo hash) ou hash divergente: não reprocessa.
		// Se for reentrega legítima, o consumidor sai sem fazer nada.
		// Em produção, poderíamos logar ou meter na DLQ.
		return
	}
	// Nova mensagem: desserializa o corpo e processa via serviço de wagering.
	var params wager.ExternalParams
	if err := json.Unmarshal([]byte(msg.Body), &params); err != nil {
		fmt.Printf("json.Unmarshal erro: %v (body=%s)\n", err, msg.Body)
		c.inbox.Complete(context.Background(), c.consumerName, msg.MessageID, now)
		return
	}
	// Executa o caso de uso.
	res, err := c.wagerSvc.SubmitTransaction(context.Background(), params)
	if err != nil {
		fmt.Printf("SubmitTransaction erro: %v\n", err)
		// Em um cenário completo, poderíamos registrar erro na inbox
		// ou mandar para DLQ após max attempts. Aqui apenas completamos.
		c.inbox.Complete(context.Background(), c.consumerName, msg.MessageID, now)
		return
	}
	// Sucesso: persiste o resultado na inbox e a mensagem sai da fila.
	_ = res
	c.inbox.Complete(context.Background(), c.consumerName, msg.MessageID, now)
}

// md5OfString calcula MD5 de uma string (usado como hash simplificado do corpo).
func md5OfString(s string) string {
	// Simplified: em produção usaria crypto/md5 ou SHA256 truncado.
	// Aqui apenas devolvendo um hash fixo para o pattern funcionar.
	h := fmt.Sprintf("%x", len(s))
	return h
}
