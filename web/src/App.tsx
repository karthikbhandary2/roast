import React from 'react';
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { AuthProvider } from './hooks/useAuth';
import { CatalogPage } from './pages/CatalogPage';
import { ProductDetailPage } from './pages/ProductDetailPage';
import { CartPage } from './pages/CartPage';
import { CheckoutPage } from './pages/CheckoutPage';
import { OrderConfirmationPage } from './pages/OrderConfirmationPage';
import { OrdersPage } from './pages/OrdersPage';
import { AuthPage } from './pages/AuthPage';
import { SellerDashboardPage } from './pages/seller/SellerDashboardPage';
import { SellerListingsPage } from './pages/seller/SellerListingsPage';
import { SellerOrdersPage } from './pages/seller/SellerOrdersPage';
import { SellerPayoutsPage } from './pages/seller/SellerPayoutsPage';
import { SellerOnboardingPage } from './pages/seller/SellerOnboardingPage';
import { AdminPage } from './pages/AdminPage';

export const App: React.FC = () => {
  return (
    <AuthProvider>
      <BrowserRouter>
        <Routes>
          <Route path="/" element={<CatalogPage />} />
          <Route path="/products/:id" element={<ProductDetailPage />} />
          <Route path="/cart" element={<CartPage />} />
          <Route path="/checkout" element={<CheckoutPage />} />
          <Route path="/orders/:id/confirmation" element={<OrderConfirmationPage />} />
          <Route path="/orders" element={<OrdersPage />} />
          <Route path="/login" element={<AuthPage />} />
          <Route path="/register" element={<AuthPage />} />
          
          {/* Seller routes */}
          <Route path="/seller/dashboard" element={<SellerDashboardPage />} />
          <Route path="/seller/listings" element={<SellerListingsPage />} />
          <Route path="/seller/orders" element={<SellerOrdersPage />} />
          <Route path="/seller/payouts" element={<SellerPayoutsPage />} />
          <Route path="/seller/onboarding" element={<SellerOnboardingPage />} />

          {/* Admin routes */}
          <Route path="/admin" element={<AdminPage />} />

          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </BrowserRouter>
    </AuthProvider>
  );
};

export default App;
