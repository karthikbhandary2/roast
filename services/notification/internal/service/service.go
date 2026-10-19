package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/roastco/ecommerce/services/notification/internal/repository"
)

type Service struct {
	repo *repository.Repository
	log  *slog.Logger
}

func New(repo *repository.Repository, log *slog.Logger) *Service {
	return &Service{
		repo: repo,
		log:  log,
	}
}

func (s *Service) Enqueue(
	ctx context.Context, recipientUserID, notifType, relatedEntityID, channel string,
) (*repository.Notification, error) {
	return s.repo.EnqueueNotification(ctx, s.repo.DB(), recipientUserID, notifType, relatedEntityID, channel)
}

func (s *Service) ProcessBatch(ctx context.Context) error {
	notifs, err := s.repo.GetQueuedNotifications(ctx, s.repo.DB(), 20)
	if err != nil {
		return err
	}

	for _, n := range notifs {
		// Deliver notification (mocking email/SMS delivery)
		s.log.Info("delivering notification",
			"id", n.ID,
			"recipient", n.RecipientUserID,
			"type", n.Type,
			"entity", n.RelatedEntityID,
			"channel", n.Channel,
		)

		// Record delivery
		if err := s.repo.MarkSent(ctx, s.repo.DB(), n.ID); err != nil {
			s.log.Warn("mark sent failed", "id", n.ID, "error", err)
			_ = s.repo.MarkFailed(ctx, s.repo.DB(), n.ID, err.Error())
		}
	}
	return nil
}

func (s *Service) StartWorker(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.ProcessBatch(ctx); err != nil {
					s.log.Warn("process notifications batch failed", "error", err)
				}
			}
		}
	}()
}

func (s *Service) HandleOrderConfirmed(ctx context.Context, orderID, buyerID string) error {
	if buyerID == "" || orderID == "" {
		return nil
	}
	_, err := s.Enqueue(ctx, buyerID, "order_confirmation", orderID, "email")
	return err
}

func (s *Service) HandleShippingUpdate(ctx context.Context, subOrderID, buyerID, trackingNumber string) error {
	if buyerID == "" || subOrderID == "" {
		return nil
	}
	_, err := s.Enqueue(ctx, buyerID, "shipping_update", subOrderID, "email")
	return err
}

func (s *Service) HandleLowStock(ctx context.Context, sellerID, productID string, remaining int32) error {
	if sellerID == "" || productID == "" {
		return nil
	}
	_, err := s.Enqueue(ctx, sellerID, "low_stock_alert", productID, "email")
	return err
}

func (s *Service) HandlePayout(ctx context.Context, sellerID, payoutID string) error {
	if sellerID == "" || payoutID == "" {
		return nil
	}
	_, err := s.Enqueue(ctx, sellerID, "payout_notification", payoutID, "email")
	return err
}

func (s *Service) HandleSellerStatus(ctx context.Context, sellerID, status, reason string) error {
	if sellerID == "" {
		return nil
	}
	notifType := fmt.Sprintf("seller_%s", status)
	_, err := s.Enqueue(ctx, sellerID, notifType, sellerID, "email")
	return err
}
