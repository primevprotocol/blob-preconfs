package listener

import (
	"context"
	"fmt"
	"log/slog"
	"math/big"
	"os"
	"time"

	"blob-preconfs/pkg/auction"

	"github.com/ethereum/go-ethereum/common"
)

type Listener struct {
	logger        *slog.Logger
	ethClient     EthClient
	relayRegistry auction.RelayRegistry

	DoneChan     chan struct{}
	NewBlockChan chan *big.Int
	// To be subscribed to by routine that'll announce winner on SL, and start settlement process.
	AuctionWonChan chan auction.SignedBid

	currentBlockNum uint64
	currentAuction  *auction.RelayAuction
}

type EthClient interface {
	BlockNumber(ctx context.Context) (uint64, error)
}

func NewListener(
	logger *slog.Logger,
	client EthClient,
	relayRegistry auction.RelayRegistry,
) *Listener {
	return &Listener{
		logger:        logger,
		ethClient:     client,
		relayRegistry: relayRegistry,

		DoneChan:       make(chan struct{}),
		NewBlockChan:   make(chan *big.Int),
		AuctionWonChan: make(chan auction.SignedBid),

		currentBlockNum: 0,
		currentAuction:  nil,
	}
}

func (l *Listener) Start(ctx context.Context) (
	doneChan chan struct{},
	auctionWonChan chan auction.SignedBid,
	err error,
) {
	l.DoneChan = make(chan struct{})
	l.NewBlockChan = make(chan *big.Int)

	go l.listenForBlocks(ctx)
	go l.processNewBlocks(ctx)

	return l.DoneChan, l.AuctionWonChan, nil
}

// Listener POC is implemented with L1 RPC polling. Websocket may be more appropriate.
func (l *Listener) listenForBlocks(ctx context.Context) {
	defer close(l.DoneChan)
	defer close(l.NewBlockChan)

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			l.logger.Info("listener stopped")
			return
		case <-ticker.C:
		}
		newBlockNum := l.MustGetBlockNum()
		if newBlockNum > l.currentBlockNum {
			l.logger.Info("new block. Signal to block processor will be sent",
				"blockNumber", l.currentBlockNum)
			l.NewBlockChan <- big.NewInt(int64(l.currentBlockNum))
			l.currentBlockNum = newBlockNum
		} else {
			l.logger.Debug("no new block. Continuing...")
		}
	}
}

func (l *Listener) MustGetBlockNum() uint64 {
	blockNumber, err := l.ethClient.BlockNumber(context.Background())
	if err != nil {
		l.logger.Error("failed to get block number", "error", err)
		os.Exit(1)
	}
	return blockNumber
}

func (l *Listener) processNewBlocks(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			l.logger.Info("block processor stopped")
			return
		case <-l.NewBlockChan:
			l.logger.Info("processing new block", "blockNumber", l.currentBlockNum)
			l.FacilitateRelayAuction()
		}
	}
}

func (l *Listener) FacilitateRelayAuction() {

	relayAuction := auction.NewRelayAuction(l.logger, l.relayRegistry)
	l.currentAuction = relayAuction
	defer func() {
		l.currentAuction = nil
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	auctionPeriod := 5 * time.Second // Adjust to whatever portion of L1 block time.
	auctionResultChan := relayAuction.StartAsync(ctx, auctionPeriod)

	select {
	case bid := <-auctionResultChan:
		zeroAddr := common.Address{}
		if bid.Address == zeroAddr {
			l.logger.Info("relay auction ended with no winner. No action to take this block")
			return
		}
		l.logger.Info("relay auction has been won", "winner", bid.Address, "amount", bid.AmountWei)
		l.AuctionWonChan <- bid
	case <-time.After(auctionPeriod + 1*time.Second):
		l.logger.Error("relay auction did not end before deadline", "error", "timeout")
		os.Exit(1)
	}
}

// To satisfy bid submissions from relays
func (l *Listener) SubmitBid(bid auction.SignedBid) error {
	if l.currentAuction == nil {
		return fmt.Errorf("no auction in progress")
	}
	if bid.L1Block.Uint64() != l.currentBlockNum {
		return fmt.Errorf("bid is for a different block")
	}
	l.currentAuction.SubmitBid(bid)
	return nil
}

// To satisfy RPC requests for current winning bid, enabling open auction.
func (l *Listener) GetCurrentBid() (winningBid auction.SignedBid, found bool) {
	if l.currentAuction == nil {
		return auction.SignedBid{}, false
	}
	return l.currentAuction.GetCurrentBid(), true
}
