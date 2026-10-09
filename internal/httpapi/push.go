package httpapi

import (
	"context"

	"github.com/brusapa/brinketask/internal/notify"
)

// GetVapidPublicKey returns the key browsers subscribe with.
func (s Server) GetVapidPublicKey(context.Context, GetVapidPublicKeyRequestObject) (GetVapidPublicKeyResponseObject, error) {
	return GetVapidPublicKey200JSONResponse{PublicKey: s.vapidPublicKey}, nil
}

// ListPushSubscriptions lists the caller's devices.
func (s Server) ListPushSubscriptions(ctx context.Context, _ ListPushSubscriptionsRequestObject) (ListPushSubscriptionsResponseObject, error) {
	userID, err := callerID(ctx)
	if err != nil {
		return nil, err
	}
	devices, err := s.devices.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	items := make([]PushSubscription, len(devices))
	for i, d := range devices {
		items[i] = deviceToAPI(d)
	}
	return ListPushSubscriptions200JSONResponse{Items: items}, nil
}

// CreatePushSubscription registers a device (D-32).
func (s Server) CreatePushSubscription(ctx context.Context, request CreatePushSubscriptionRequestObject) (CreatePushSubscriptionResponseObject, error) {
	userID, err := callerID(ctx)
	if err != nil {
		return nil, err
	}
	body := request.Body
	device, created, err := s.devices.Register(ctx, userID, notify.NewDevice{
		ID:       body.Id,
		Endpoint: body.Endpoint,
		P256dh:   body.Keys.P256dh,
		Auth:     body.Keys.Auth,
		Label:    nullableToPointer(body.Label),
	})
	if problem, ok := domainProblem(err); ok {
		return CreatePushSubscriptiondefaultApplicationProblemPlusJSONResponse(toResponse(problem)), nil
	}
	if err != nil {
		return nil, err
	}
	if created {
		return CreatePushSubscription201JSONResponse(deviceToAPI(device)), nil
	}
	return CreatePushSubscription200JSONResponse(deviceToAPI(device)), nil
}

// DeletePushSubscription removes a device for good (D-32).
func (s Server) DeletePushSubscription(ctx context.Context, request DeletePushSubscriptionRequestObject) (DeletePushSubscriptionResponseObject, error) {
	userID, err := callerID(ctx)
	if err != nil {
		return nil, err
	}
	err = s.devices.Delete(ctx, userID, request.Id)
	if problem, ok := domainProblem(err); ok {
		return DeletePushSubscriptiondefaultApplicationProblemPlusJSONResponse(toResponse(problem)), nil
	}
	if err != nil {
		return nil, err
	}
	return DeletePushSubscription204Response{}, nil
}

// TestPushSubscription sends a test notification to a device.
func (s Server) TestPushSubscription(ctx context.Context, request TestPushSubscriptionRequestObject) (TestPushSubscriptionResponseObject, error) {
	userID, err := callerID(ctx)
	if err != nil {
		return nil, err
	}
	err = s.devices.SendTest(ctx, userID, request.Id)
	if problem, ok := domainProblem(err); ok {
		return TestPushSubscriptiondefaultApplicationProblemPlusJSONResponse(toResponse(problem)), nil
	}
	if err != nil {
		return nil, err
	}
	return TestPushSubscription202Response{}, nil
}

func deviceToAPI(d notify.Device) PushSubscription {
	return PushSubscription{
		Id:            d.ID,
		Channel:       PushChannel(d.Channel),
		Label:         pointerToNullable(d.Label),
		CreatedAt:     d.CreatedAt.UTC(),
		LastSuccessAt: utcNullable(d.LastSuccessAt),
		DisabledAt:    utcNullable(d.DisabledAt),
	}
}
