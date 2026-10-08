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

func (Server) DeleteChecklistItem(context.Context, DeleteChecklistItemRequestObject) (DeleteChecklistItemResponseObject, error) {
	return DeleteChecklistItemdefaultApplicationProblemPlusJSONResponse(notImplemented()), nil
}

func (Server) PatchChecklistItem(context.Context, PatchChecklistItemRequestObject) (PatchChecklistItemResponseObject, error) {
	return PatchChecklistItemdefaultApplicationProblemPlusJSONResponse(notImplemented()), nil
}

func (Server) ListCompletions(context.Context, ListCompletionsRequestObject) (ListCompletionsResponseObject, error) {
	return ListCompletionsdefaultApplicationProblemPlusJSONResponse(notImplemented()), nil
}

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

func (Server) DeleteReminder(context.Context, DeleteReminderRequestObject) (DeleteReminderResponseObject, error) {
	return DeleteReminderdefaultApplicationProblemPlusJSONResponse(notImplemented()), nil
}

func (Server) PatchReminder(context.Context, PatchReminderRequestObject) (PatchReminderResponseObject, error) {
	return PatchReminderdefaultApplicationProblemPlusJSONResponse(notImplemented()), nil
}

func (Server) GetChanges(context.Context, GetChangesRequestObject) (GetChangesResponseObject, error) {
	return GetChangesdefaultApplicationProblemPlusJSONResponse(notImplemented()), nil
}

func (Server) ListTags(context.Context, ListTagsRequestObject) (ListTagsResponseObject, error) {
	return ListTagsdefaultApplicationProblemPlusJSONResponse(notImplemented()), nil
}

func (Server) CreateTag(context.Context, CreateTagRequestObject) (CreateTagResponseObject, error) {
	return CreateTagdefaultApplicationProblemPlusJSONResponse(notImplemented()), nil
}

func (Server) DeleteTag(context.Context, DeleteTagRequestObject) (DeleteTagResponseObject, error) {
	return DeleteTagdefaultApplicationProblemPlusJSONResponse(notImplemented()), nil
}

func (Server) PatchTag(context.Context, PatchTagRequestObject) (PatchTagResponseObject, error) {
	return PatchTagdefaultApplicationProblemPlusJSONResponse(notImplemented()), nil
}

func (Server) ListTasks(context.Context, ListTasksRequestObject) (ListTasksResponseObject, error) {
	return ListTasksdefaultApplicationProblemPlusJSONResponse(notImplemented()), nil
}

func (Server) CreateTask(context.Context, CreateTaskRequestObject) (CreateTaskResponseObject, error) {
	return CreateTaskdefaultApplicationProblemPlusJSONResponse(notImplemented()), nil
}

func (Server) DeleteTask(context.Context, DeleteTaskRequestObject) (DeleteTaskResponseObject, error) {
	return DeleteTaskdefaultApplicationProblemPlusJSONResponse(notImplemented()), nil
}

func (Server) GetTask(context.Context, GetTaskRequestObject) (GetTaskResponseObject, error) {
	return GetTaskdefaultApplicationProblemPlusJSONResponse(notImplemented()), nil
}

func (Server) PatchTask(context.Context, PatchTaskRequestObject) (PatchTaskResponseObject, error) {
	return PatchTaskdefaultApplicationProblemPlusJSONResponse(notImplemented()), nil
}

func (Server) CreateChecklistItem(context.Context, CreateChecklistItemRequestObject) (CreateChecklistItemResponseObject, error) {
	return CreateChecklistItemdefaultApplicationProblemPlusJSONResponse(notImplemented()), nil
}

func (Server) CompleteTask(context.Context, CompleteTaskRequestObject) (CompleteTaskResponseObject, error) {
	return CompleteTaskdefaultApplicationProblemPlusJSONResponse(notImplemented()), nil
}

func (Server) ListTaskCompletions(context.Context, ListTaskCompletionsRequestObject) (ListTaskCompletionsResponseObject, error) {
	return ListTaskCompletionsdefaultApplicationProblemPlusJSONResponse(notImplemented()), nil
}

func (Server) CreateReminder(context.Context, CreateReminderRequestObject) (CreateReminderResponseObject, error) {
	return CreateReminderdefaultApplicationProblemPlusJSONResponse(notImplemented()), nil
}

func (Server) RestoreTask(context.Context, RestoreTaskRequestObject) (RestoreTaskResponseObject, error) {
	return RestoreTaskdefaultApplicationProblemPlusJSONResponse(notImplemented()), nil
}

func (Server) SkipTaskOccurrence(context.Context, SkipTaskOccurrenceRequestObject) (SkipTaskOccurrenceResponseObject, error) {
	return SkipTaskOccurrencedefaultApplicationProblemPlusJSONResponse(notImplemented()), nil
}

func (Server) SnoozeTask(context.Context, SnoozeTaskRequestObject) (SnoozeTaskResponseObject, error) {
	return SnoozeTaskdefaultApplicationProblemPlusJSONResponse(notImplemented()), nil
}

func (Server) UncompleteTask(context.Context, UncompleteTaskRequestObject) (UncompleteTaskResponseObject, error) {
	return UncompleteTaskdefaultApplicationProblemPlusJSONResponse(notImplemented()), nil
}
