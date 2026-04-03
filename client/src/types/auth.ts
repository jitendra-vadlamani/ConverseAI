export interface User {
  id: string;
  email: string;
  created_at: string;
}

export interface AuthResponse {
  message: string;
  user: User;
}

export interface AuthError {
  message: string;
}
