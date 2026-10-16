export interface ChoiceOption {
  value: string
  label: string
  disabled?: boolean
}
export interface MenuAction {
  id: string
  label: string
  disabled?: boolean
  danger?: boolean
}
export interface TreeNode {
  id: string
  label: string
  children?: TreeNode[]
  expandable?: boolean
  disabled?: boolean
}
export interface TableColumn {
  key: string
  label: string
}
export interface TableRow {
  id: string
  [key: string]: unknown
}
export type CloseReason = 'escape' | 'outside' | 'action'
export type ButtonState = 'idle' | 'loading' | 'success'
