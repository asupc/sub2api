/**
 * API Client for Sub2API Backend
 * Central export point for all API modules
 */

// Re-export the HTTP client
export { apiClient } from './client'

// Auth API
export { authAPI, type LoginResponse } from './auth'

// User APIs
export { keysAPI } from './keys'
export { usageAPI } from './usage'
export { userAPI } from './user'
export { userGroupsAPI } from './groups'
export * as batchImageAPI from './batchImage'
export { channelMonitorUserAPI } from './channelMonitor'

// Admin APIs
export { adminAPI } from './admin'

// Default export
export { default } from './client'
