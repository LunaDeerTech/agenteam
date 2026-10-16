import type { InjectionKey, Ref } from "vue";

export const settingsActionsTarget: InjectionKey<
  Readonly<Ref<HTMLElement | undefined>>
> = Symbol("settings-page-actions");
