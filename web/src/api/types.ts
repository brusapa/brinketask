// Short names for the generated contract types (schema.gen.ts is generated
// from api/openapi.yaml and never edited).
import type { components } from "./schema.gen";

type Schemas = components["schemas"];

export type User = Schemas["User"];
export type UserPatch = Schemas["UserPatch"];
export type List = Schemas["List"];
export type Task = Schemas["Task"];
export type ChecklistItem = Schemas["ChecklistItem"];
export type Tag = Schemas["Tag"];
export type ChangesPage = Schemas["ChangesPage"];
export type CompletionEntry = Schemas["CompletionEntry"];
export type CompletionResult = Schemas["CompletionResult"];
export type Problem = Schemas["Problem"];
export type ProblemCode = Problem["code"];
export type TaskCreate = Schemas["TaskCreate"];
export type TaskPatch = Schemas["TaskPatch"];
export type ListCreate = Schemas["ListCreate"];
export type ListPatch = Schemas["ListPatch"];
export type TagPatch = Schemas["TagPatch"];
export type ChecklistItemPatch = Schemas["ChecklistItemPatch"];
export type Reminder = Schemas["Reminder"];
export type PushSubscription = Schemas["PushSubscription"];
