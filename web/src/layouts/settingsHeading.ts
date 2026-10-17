import {
  inject,
  onBeforeUnmount,
  watchEffect,
  type InjectionKey,
  type Ref,
} from "vue";
export interface SettingsHeading {
  title: string;
  back?: { to: string; label: string };
}
export const settingsHeading: InjectionKey<Ref<SettingsHeading | undefined>> =
  Symbol("settings-heading");
export function useSettingsHeading(value: () => SettingsHeading) {
  const heading = inject(settingsHeading);
  let owned: SettingsHeading | undefined;
  watchEffect(() => {
    if (heading) heading.value = owned = value();
  });
  onBeforeUnmount(() => {
    if (heading && heading.value === owned) heading.value = undefined;
  });
}
