export function emailError(value: string) {
  return /^[\x21-\x7e]+@[^@\s]+\.[^@\s]+$/.test(value) && value.length <= 254
    ? ""
    : "请输入有效的邮箱地址。";
}
export function usernameError(value: string) {
  return /^[a-zA-Z0-9](?:[a-zA-Z0-9-]{1,30})[a-zA-Z0-9]$/.test(value)
    ? ""
    : "使用 3–32 位字母、数字或中间连字符。";
}
export function displayNameError(value: string) {
  return [...value].length <= 80 &&
    new TextEncoder().encode(value).length <= 320 &&
    !/[\u0000-\u001f\u007f]/.test(value)
    ? ""
    : "显示名最多 80 个字符，不能包含控制字符。";
}
export function passwordError(value: string) {
  return [...value].length >= 15 &&
    [...value].length <= 128 &&
    new TextEncoder().encode(value).length <= 512
    ? ""
    : "密码需要 15–128 个字符，且不超过 512 字节。";
}
