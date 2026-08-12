import * as React from "react";
import { api, storeTokens, clearTokens, getAccessToken, getRefreshToken, type User } from "@/lib/api";

interface AuthContextValue {
  user: User | null;
  loading: boolean;
  login: (email: string, password: string) => Promise<void>;
  register: (email: string, password: string, fullName: string) => Promise<void>;
  logout: () => Promise<void>;
}

const AuthContext = React.createContext<AuthContextValue | undefined>(undefined);

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [user, setUser] = React.useState<User | null>(null);
  const [loading, setLoading] = React.useState(true);

  React.useEffect(() => {
    if (getAccessToken()) {
      api
        .accounts()
        .then(() => {
          const stored = localStorage.getItem("hnl_user");
          if (stored) {
            setUser(JSON.parse(stored));
          }
        })
        .catch(() => {
          clearTokens();
          localStorage.removeItem("hnl_user");
          setUser(null);
        })
        .finally(() => setLoading(false));
    } else {
      setLoading(false);
    }
  }, []);

  const login = React.useCallback(async (email: string, password: string) => {
    const res = await api.login({ email, password });
    storeTokens(res.tokens);
    localStorage.setItem("hnl_user", JSON.stringify(res.user));
    setUser(res.user);
  }, []);

  const register = React.useCallback(async (email: string, password: string, fullName: string) => {
    const res = await api.register({ email, password, full_name: fullName });
    storeTokens(res.tokens);
    localStorage.setItem("hnl_user", JSON.stringify(res.user));
    setUser(res.user);
  }, []);

  const logout = React.useCallback(async () => {
    const refresh = getRefreshToken();
    if (refresh) {
      try {
        await api.logout(refresh);
      } catch {
        // ignore logout errors; clear local state regardless
      }
    }
    clearTokens();
    localStorage.removeItem("hnl_user");
    setUser(null);
  }, []);

  return (
    <AuthContext.Provider value={{ user, loading, login, register, logout }}>
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth(): AuthContextValue {
  const ctx = React.useContext(AuthContext);
  if (!ctx) {
    throw new Error("useAuth must be used within AuthProvider");
  }
  return ctx;
}
