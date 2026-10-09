package httpapi

import "context"

// Phase 0 stubs: every operation of the contract answers 501 Not Implemented
// until the phase that implements it (SPEC section 12). Implementing an
// operation means deleting its stub here and writing the real method in the
// file of its resource; the compiler then flags any operation left without
// a method, because Server must satisfy StrictServerInterface.
//
// Each generated "...defaultApplicationProblemPlusJSONResponse" type has
// exactly the fields of problemResponse, so Go allows converting one into the
// other with T(value).

func (Server) ListPushSubscriptions(context.Context, ListPushSubscriptionsRequestObject) (ListPushSubscriptionsResponseObject, error) {
	return ListPushSubscriptionsdefaultApplicationProblemPlusJSONResponse(notImplemented()), nil
}

func (Server) CreatePushSubscription(context.Context, CreatePushSubscriptionRequestObject) (CreatePushSubscriptionResponseObject, error) {
	return CreatePushSubscriptiondefaultApplicationProblemPlusJSONResponse(notImplemented()), nil
}

func (Server) DeletePushSubscription(context.Context, DeletePushSubscriptionRequestObject) (DeletePushSubscriptionResponseObject, error) {
	return DeletePushSubscriptiondefaultApplicationProblemPlusJSONResponse(notImplemented()), nil
}

func (Server) TestPushSubscription(context.Context, TestPushSubscriptionRequestObject) (TestPushSubscriptionResponseObject, error) {
	return TestPushSubscriptiondefaultApplicationProblemPlusJSONResponse(notImplemented()), nil
}

func (Server) GetVapidPublicKey(context.Context, GetVapidPublicKeyRequestObject) (GetVapidPublicKeyResponseObject, error) {
	return GetVapidPublicKeydefaultApplicationProblemPlusJSONResponse(notImplemented()), nil
}
