import React from "react";
import ReactDOM from "react-dom/client";

import { AuthGate } from "./AuthGate";
import "./styles.css";

const root = document.getElementById("root");

if (!root) {
  throw new Error("Missing application root");
}

ReactDOM.createRoot(root).render(
  <React.StrictMode>
    <AuthGate />
  </React.StrictMode>,
);
