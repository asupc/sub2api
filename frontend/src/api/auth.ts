/**
 * Authentication API endpoints
 * Handles user login, registration, and logout operations
 */

import { apiClient } from './client'
import { refreshAuthTokens, type RefreshTokenResponse } from './tokenRefresh'
export type { RefreshTokenResponse } from './tokenRefresh'
import type {
  LoginRequest,
  RegisterRequest,
  AuthResponse,
  CurrentUserResponse,
  SendVerifyCodeRequest,
  SendVerifyCodeResponse,
  PublicSettings
} from '@/types'

/**
 * Login response type
 */
export type LoginResponse = AuthResponse

/**
 * Store authentication token in localStorage
 */
export function setAuthToken(token: string): void {
  localStorage.setItem('auth_token', token)
}

/**
 * Store refresh token in localStorage
 */
export function setRefreshToken(token: string): void {
  localStorage.setItem('refresh_token', token)
}

/**
 * Store token expiration timestamp in localStorage
 * Converts expires_in (seconds) to absolute timestamp (milliseconds)
 */
export function setTokenExpiresAt(expiresIn: number): void {
  const expiresAt = Date.now() + expiresIn * 1000
  localStorage.setItem('token_expires_at', String(expiresAt))
}

/**
 * Get authentication token from localStorage
 */
export function getAuthToken(): string | null {
  return localStorage.getItem('auth_token')
}

/**
 * Get refresh token from localStorage
 */
export function getRefreshToken(): string | null {
  return localStorage.getItem('refresh_token')
}

/**
 * Get token expiration timestamp from localStorage
 */
export function getTokenExpiresAt(): number | null {
  const value = localStorage.getItem('token_expires_at')
  return value ? parseInt(value, 10) : null
}

/**
 * Clear authentication token from localStorage
 */
export function clearAuthToken(): void {
  localStorage.removeItem('auth_token')
  localStorage.removeItem('refresh_token')
  localStorage.removeItem('auth_user')
  localStorage.removeItem('token_expires_at')
}

/**
 * User login
 * @param credentials - Email and password
 * @returns Authentication response with token and user data
 */
export async function login(credentials: LoginRequest): Promise<AuthResponse> {
  const { data } = await apiClient.post<AuthResponse>('/auth/login', credentials)

  // Store token and user data
  setAuthToken(data.access_token)
  if (data.refresh_token) {
    setRefreshToken(data.refresh_token)
  }
  if (data.expires_in) {
    setTokenExpiresAt(data.expires_in)
  }
  localStorage.setItem('auth_user', JSON.stringify(data.user))

  return data
}

/**
 * User registration
 * @param userData - Registration data (username, email, password)
 * @returns Authentication response with token and user data
 */
export async function register(userData: RegisterRequest): Promise<AuthResponse> {
  const { data } = await apiClient.post<AuthResponse>('/auth/register', userData)

  // Store token and user data
  setAuthToken(data.access_token)
  if (data.refresh_token) {
    setRefreshToken(data.refresh_token)
  }
  if (data.expires_in) {
    setTokenExpiresAt(data.expires_in)
  }
  localStorage.setItem('auth_user', JSON.stringify(data.user))

  return data
}

/**
 * Get current authenticated user
 * @returns User profile data
 */
export async function getCurrentUser() {
  return apiClient.get<CurrentUserResponse>('/auth/me')
}

/**
 * User logout
 * Clears authentication token and user data from localStorage
 * Optionally revokes the refresh token on the server
 */
export async function logout(): Promise<void> {
  const refreshToken = getRefreshToken()

  // Try to revoke the refresh token on the server
  if (refreshToken) {
    try {
      await apiClient.post('/auth/logout', { refresh_token: refreshToken })
    } catch {
      // Ignore errors - we still want to clear local state
    }
  }

  clearAuthToken()
}

/**
 * Refresh the access token using the refresh token
 * @returns New token pair
 */
export async function refreshToken(): Promise<RefreshTokenResponse> {
  return refreshAuthTokens()
}

/**
 * Revoke all sessions for the current user
 * @returns Response with message
 */
export async function revokeAllSessions(): Promise<{ message: string }> {
  const { data } = await apiClient.post<{ message: string }>('/auth/revoke-all-sessions')
  return data
}

/**
 * Check if user is authenticated
 * @returns True if user has valid token
 */
export function isAuthenticated(): boolean {
  return getAuthToken() !== null
}

/**
 * Get public settings (no auth required)
 * @returns Public settings including registration and captcha config
 */
export async function getPublicSettings(): Promise<PublicSettings> {
  const { data } = await apiClient.get<PublicSettings>('/settings/public')
  return data
}

/**
 * Send verification code to email
 * @param request - Email and optional captcha token
 * @returns Response with countdown seconds
 */
export async function sendVerifyCode(
  request: SendVerifyCodeRequest
): Promise<SendVerifyCodeResponse> {
  const { data } = await apiClient.post<SendVerifyCodeResponse>('/auth/send-verify-code', request)
  return data
}

/**
 * Forgot password request
 */
export interface ForgotPasswordRequest {
  email: string
  turnstile_token?: string
  tencent_captcha_ticket?: string
  tencent_captcha_randstr?: string
}

/**
 * Forgot password response
 */
export interface ForgotPasswordResponse {
  message: string
}

/**
 * Request password reset link
 * @param request - Email and optional Turnstile token
 * @returns Response with message
 */
export async function forgotPassword(request: ForgotPasswordRequest): Promise<ForgotPasswordResponse> {
  const { data } = await apiClient.post<ForgotPasswordResponse>('/auth/forgot-password', request)
  return data
}

/**
 * Reset password request
 */
export interface ResetPasswordRequest {
  email: string
  token: string
  new_password: string
}

/**
 * Reset password response
 */
export interface ResetPasswordResponse {
  message: string
}

/**
 * Reset password with token
 * @param request - Email, token, and new password
 * @returns Response with message
 */
export async function resetPassword(request: ResetPasswordRequest): Promise<ResetPasswordResponse> {
  const { data } = await apiClient.post<ResetPasswordResponse>('/auth/reset-password', request)
  return data
}

export const authAPI = {
  login,
  register,
  getCurrentUser,
  logout,
  isAuthenticated,
  setAuthToken,
  setRefreshToken,
  setTokenExpiresAt,
  getAuthToken,
  getRefreshToken,
  getTokenExpiresAt,
  clearAuthToken,
  getPublicSettings,
  sendVerifyCode,
  forgotPassword,
  resetPassword,
  refreshToken,
  revokeAllSessions
}

export default authAPI
