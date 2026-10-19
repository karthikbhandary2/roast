import React, { createContext, useContext, useState, useEffect } from 'react';
import { api } from '../api/client';
import { User } from '../types';

interface AuthContextType {
  user: User | null;
  token: string | null;
  login: (email: string, password: string) => Promise<void>;
  register: (email: string, password: string, displayName: string) => Promise<void>;
  logout: () => void;
  isSeller: boolean;
  isAdmin: boolean;
  loading: boolean;
}

const AuthContext = createContext<AuthContextType>({} as AuthContextType);

function parseJwt(token: string): any {
  try {
    const base64Url = token.split('.')[1];
    const base64 = base64Url.replace(/-/g, '+').replace(/_/g, '/');
    const jsonPayload = decodeURIComponent(
      window
        .atob(base64)
        .split('')
        .map((c) => '%' + ('00' + c.charCodeAt(0).toString(16)).slice(-2))
        .join('')
    );
    return JSON.parse(jsonPayload);
  } catch {
    return null;
  }
}

export const AuthProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const [token, setToken] = useState<string | null>(localStorage.getItem('token'));
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    if (token) {
      const claims = parseJwt(token);
      if (claims && (!claims.exp || claims.exp * 1000 > Date.now())) {
        setUser({
          id: claims.user_id || claims.sub,
          email: claims.email || '',
          display_name: claims.display_name || 'Coffee Connoisseur',
          roles: claims.roles || [],
        });
      } else {
        localStorage.removeItem('token');
        setToken(null);
        setUser(null);
      }
    } else {
      setUser(null);
    }
    setLoading(false);
  }, [token]);

  const login = async (email: string, password: string) => {
    const res = await api.login({ email, password });
    if (res?.access_token) {
      localStorage.setItem('token', res.access_token);
      setToken(res.access_token);
    }
  };

  const register = async (email: string, password: string, displayName: string) => {
    await api.register({ email, password, display_name: displayName });
    await login(email, password);
  };

  const logout = () => {
    localStorage.removeItem('token');
    setToken(null);
    setUser(null);
  };

  const isSeller = Boolean(user?.roles?.includes('seller') || user?.roles?.includes('ROLE_SELLER'));
  const isAdmin = Boolean(user?.roles?.includes('admin') || user?.roles?.includes('ROLE_ADMIN'));

  return (
    <AuthContext.Provider value={{ user, token, login, register, logout, isSeller, isAdmin, loading }}>
      {children}
    </AuthContext.Provider>
  );
};

export const useAuth = () => useContext(AuthContext);
