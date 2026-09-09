import React, { createContext, useContext, useState, ReactNode } from "react";
import { UploadResponse } from "./types";

// Define the shape of the context state
interface AppContextType {
  upload: UploadResponse | undefined;
  setUpload: (value: UploadResponse | undefined) => void;
}

// Create the context with a default value of undefined
const AppContext = createContext<AppContextType | undefined>(undefined);

// Define the props for the provider
interface AppProviderProps {
  children: ReactNode;
}

// Provider component
export const AppProvider: React.FC<AppProviderProps> = ({ children }) => {
  const [upload, setUpload] = useState<UploadResponse | undefined>();

  return (
    <AppContext.Provider value={{ upload, setUpload }}>
      {children}
    </AppContext.Provider>
  );
};

// Custom hook for consuming the context
export const useAppContext = (): AppContextType => {
  const context = useContext(AppContext);
  if (!context) {
    throw new Error("useAppContext must be used within an AppProvider");
  }
  return context;
};
