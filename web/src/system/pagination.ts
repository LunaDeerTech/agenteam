export const pageSizes = [5, 10, 20, 50] as const;
export type PageSize = (typeof pageSizes)[number];
export const pageSizeChoices = pageSizes.map((size) => ({
  value: String(size),
  label: `${size} 条`,
}));
export function isPageSize(value: unknown): value is PageSize {
  return typeof value === "number" && pageSizes.some((size) => size === value);
}
