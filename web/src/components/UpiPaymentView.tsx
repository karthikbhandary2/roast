import React, { useState, useEffect, useRef } from 'react';
import QRCode from 'qrcode';
import { api } from '../api/client';
import { Order } from '../types';

interface UpiPaymentViewProps {
  order: Order;
  vpa?: string;
  onPaymentSuccess?: () => void;
  onPaymentFailed?: (reason: string) => void;
}

export const UpiPaymentView: React.FC<UpiPaymentViewProps> = ({
  order,
  vpa = 'buyer@okhdfcbank',
  onPaymentSuccess,
  onPaymentFailed,
}) => {
  const [tab, setTab] = useState<'qr' | 'collect'>('qr');
  const [timeLeft, setTimeLeft] = useState(300); // 5 minutes countdown
  const [simulating, setSimulating] = useState(false);
  const [autoApprove, setAutoApprove] = useState(false);
  const [autoApproveCountdown, setAutoApproveCountdown] = useState<number | null>(null);
  const [utrNumber, setUtrNumber] = useState<string | null>(null);
  const [pinInput, setPinInput] = useState('');
  const [showPinModal, setShowPinModal] = useState(false);
  const [pinError, setPinError] = useState(false);

  const canvasRef = useRef<HTMLCanvasElement>(null);

  const subOrder = order.sub_orders?.[0];
  const subOrderId = subOrder?.id;
  const totalMinor = Number(order.total?.amount_minor || 0);
  const formattedAmount = (totalMinor / 100).toFixed(2);

  // Standard NPCI UPI URI scheme
  const upiUri = `upi://pay?pa=roastco@icici&pn=RoastCo%20Specialty%20Coffee&am=${formattedAmount}&cu=INR&tn=${encodeURIComponent(`Order #${order.id.slice(0, 8)}`)}&tr=${subOrderId || order.id}`;

  // Generate QR Code on canvas
  useEffect(() => {
    if (canvasRef.current) {
      QRCode.toCanvas(canvasRef.current, upiUri, {
        width: 220,
        margin: 2,
        color: {
          dark: '#1e1c1b',
          light: '#ffffff',
        },
      }).catch((err) => console.error('QR code generation failed:', err));
    }
  }, [upiUri, tab]);

  // Countdown timer for 5 minutes
  useEffect(() => {
    if (timeLeft <= 0) return;
    const timer = setInterval(() => {
      setTimeLeft((prev) => prev - 1);
    }, 1000);
    return () => clearInterval(timer);
  }, [timeLeft]);

  // Auto-approve countdown effect
  useEffect(() => {
    if (!autoApprove) {
      setAutoApproveCountdown(null);
      return;
    }
    setAutoApproveCountdown(5);
    const interval = setInterval(() => {
      setAutoApproveCountdown((prev) => {
        if (prev === null || prev <= 1) {
          clearInterval(interval);
          handleApprovePayment();
          return 0;
        }
        return prev - 1;
      });
    }, 1000);
    return () => clearInterval(interval);
  }, [autoApprove]);

  const handleApprovePayment = async () => {
    if (!subOrderId) return;
    setSimulating(true);
    setShowPinModal(false);
    try {
      const generatedUTR = Math.floor(100000000000 + Math.random() * 900000000000).toString();
      setUtrNumber(generatedUTR);
      await api.simulateUPI(subOrderId, 'approve');
      if (onPaymentSuccess) {
        onPaymentSuccess();
      }
    } catch (err: any) {
      console.error('Simulate UPI failed', err);
      if (onPaymentFailed) {
        onPaymentFailed(err.message || 'Payment simulation failed');
      }
    } finally {
      setSimulating(false);
    }
  };

  const handleDeclinePayment = async () => {
    if (!subOrderId) return;
    setSimulating(true);
    try {
      await api.simulateUPI(subOrderId, 'decline');
      if (onPaymentFailed) {
        onPaymentFailed('Payment declined by user in UPI app.');
      }
    } catch (err: any) {
      console.error('Decline UPI failed', err);
    } finally {
      setSimulating(false);
    }
  };

  const formatTimer = (seconds: number) => {
    const mins = Math.floor(seconds / 60);
    const secs = seconds % 60;
    return `${mins.toString().padStart(2, '0')}:${secs.toString().padStart(2, '0')}`;
  };

  const upiApps = [
    { name: 'Google Pay', icon: 'ti-brand-google', color: '#4285F4' },
    { name: 'PhonePe', icon: 'ti-device-mobile', color: '#5f259f' },
    { name: 'Paytm', icon: 'ti-wallet', color: '#00b9f5' },
    { name: 'BHIM', icon: 'ti-bolt', color: '#00833d' },
    { name: 'CRED', icon: 'ti-shield-check', color: '#111111' },
  ];

  return (
    <div style={{
      background: 'var(--surface-1)',
      border: '1px solid var(--border)',
      borderRadius: 'var(--radius-xl)',
      padding: '24px',
      boxShadow: 'var(--shadow-md)',
      maxWidth: '520px',
      margin: '0 auto',
    }}>
      {/* Header with authentic UPI Branding */}
      <div style={{
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'space-between',
        paddingBottom: '16px',
        borderBottom: '1px solid var(--border)',
        marginBottom: '20px',
      }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '10px' }}>
          <div style={{
            background: 'linear-gradient(135deg, #097939 0%, #008744 50%, #f37023 100%)',
            color: '#fff',
            fontWeight: 800,
            fontSize: '11px',
            padding: '4px 8px',
            borderRadius: '4px',
            letterSpacing: '0.5px',
          }}>
            UPI ⚡
          </div>
          <div>
            <div style={{ fontWeight: 700, fontSize: '15px', color: 'var(--text-primary)' }}>
              Unified Payments Interface
            </div>
            <div style={{ fontSize: '11px', color: 'var(--text-muted)' }}>
              Instant & Secure 24x7 Bank Transfer
            </div>
          </div>
        </div>

        <div style={{ textAlign: 'right' }}>
          <div style={{ fontSize: '18px', fontWeight: 800, color: 'var(--text-primary)' }}>
            ₹{formattedAmount}
          </div>
          <div style={{ fontSize: '11px', color: 'var(--text-muted)' }}>
            Order #{order.id.slice(0, 8)}
          </div>
          {utrNumber && (
            <div style={{ fontSize: '11px', color: 'var(--text-success)', fontWeight: 600 }}>
              UTR: {utrNumber}
            </div>
          )}
        </div>
      </div>

      {/* Tabs: Scan QR vs UPI Collect ID */}
      <div style={{
        display: 'flex',
        background: 'var(--surface-2)',
        padding: '4px',
        borderRadius: 'var(--radius)',
        marginBottom: '20px',
      }}>
        <button
          type="button"
          onClick={() => setTab('qr')}
          style={{
            flex: 1,
            padding: '8px',
            fontSize: '12px',
            fontWeight: tab === 'qr' ? 700 : 500,
            borderRadius: 'calc(var(--radius) - 2px)',
            background: tab === 'qr' ? 'var(--surface-1)' : 'transparent',
            border: 'none',
            color: tab === 'qr' ? 'var(--text-primary)' : 'var(--text-muted)',
            boxShadow: tab === 'qr' ? '0 1px 3px rgba(0,0,0,0.08)' : 'none',
            cursor: 'pointer',
          }}
        >
          📱 Scan QR Code
        </button>
        <button
          type="button"
          onClick={() => setTab('collect')}
          style={{
            flex: 1,
            padding: '8px',
            fontSize: '12px',
            fontWeight: tab === 'collect' ? 700 : 500,
            borderRadius: 'calc(var(--radius) - 2px)',
            background: tab === 'collect' ? 'var(--surface-1)' : 'transparent',
            border: 'none',
            color: tab === 'collect' ? 'var(--text-primary)' : 'var(--text-muted)',
            boxShadow: tab === 'collect' ? '0 1px 3px rgba(0,0,0,0.08)' : 'none',
            cursor: 'pointer',
          }}
        >
          📩 UPI Collect / VPA
        </button>
      </div>

      {/* Tab 1: Dynamic QR Code */}
      {tab === 'qr' && (
        <div style={{ textAlign: 'center', marginBottom: '20px' }}>
          <div style={{
            display: 'inline-block',
            padding: '12px',
            background: '#ffffff',
            borderRadius: '12px',
            border: '1px solid var(--border)',
            boxShadow: '0 4px 12px rgba(0,0,0,0.05)',
            marginBottom: '12px',
          }}>
            <canvas ref={canvasRef} style={{ display: 'block' }} />
          </div>

          <div style={{
            fontSize: '13px',
            fontWeight: 600,
            color: 'var(--text-primary)',
            marginBottom: '4px',
          }}>
            Scan with any UPI App
          </div>
          <div style={{ fontSize: '11px', color: 'var(--text-muted)', marginBottom: '16px' }}>
            Open Google Pay, PhonePe, Paytm, BHIM, or your mobile banking app
          </div>

          {/* Supported Apps Pills */}
          <div style={{
            display: 'flex',
            justifyContent: 'center',
            alignItems: 'center',
            gap: '8px',
            flexWrap: 'wrap',
            marginBottom: '16px',
          }}>
            {upiApps.map((app) => (
              <span
                key={app.name}
                style={{
                  display: 'inline-flex',
                  alignItems: 'center',
                  gap: '4px',
                  background: 'var(--surface-2)',
                  border: '1px solid var(--border)',
                  borderRadius: '16px',
                  padding: '4px 8px',
                  fontSize: '11px',
                  color: 'var(--text-secondary)',
                  fontWeight: 500,
                }}
              >
                <i className={`ti ${app.icon}`} style={{ color: app.color, fontSize: '12px' }} />
                {app.name}
              </span>
            ))}
          </div>

          {/* Deep link button for mobile devices */}
          <a
            href={upiUri}
            className="btn-secondary"
            style={{
              display: 'inline-flex',
              alignItems: 'center',
              gap: '6px',
              fontSize: '12px',
              padding: '6px 14px',
              textDecoration: 'none',
              borderRadius: 'var(--radius)',
            }}
          >
            <i className="ti ti-external-link" /> Open in UPI App
          </a>
        </div>
      )}

      {/* Tab 2: VPA Collect Request */}
      {tab === 'collect' && (
        <div style={{
          textAlign: 'center',
          padding: '20px 10px',
          marginBottom: '20px',
        }}>
          <div style={{
            width: '56px',
            height: '56px',
            borderRadius: '50%',
            background: 'var(--badge-warning-bg)',
            color: 'var(--fill-accent)',
            display: 'inline-flex',
            alignItems: 'center',
            justifyContent: 'center',
            fontSize: '26px',
            marginBottom: '12px',
          }}>
            <i className="ti ti-bell-ringing ti-spin" />
          </div>

          <div style={{ fontSize: '14px', fontWeight: 700, marginBottom: '4px' }}>
            Collect Request Dispatched
          </div>
          <div style={{ fontSize: '12px', color: 'var(--text-secondary)', marginBottom: '16px' }}>
            A payment request of <strong style={{ color: 'var(--text-primary)' }}>₹{formattedAmount}</strong> has been sent to:
          </div>

          <div style={{
            display: 'inline-flex',
            alignItems: 'center',
            gap: '8px',
            background: 'var(--surface-2)',
            border: '1px solid var(--border)',
            padding: '8px 16px',
            borderRadius: 'var(--radius)',
            fontSize: '14px',
            fontWeight: 600,
            color: 'var(--text-primary)',
            marginBottom: '16px',
          }}>
            <i className="ti ti-at" style={{ color: 'var(--fill-accent)' }} />
            {vpa}
            <span style={{
              background: 'var(--badge-success-bg)',
              color: 'var(--text-success)',
              fontSize: '10px',
              padding: '2px 6px',
              borderRadius: '4px',
              fontWeight: 700,
            }}>
              ✓ VERIFIED
            </span>
          </div>

          <div style={{ fontSize: '12px', color: 'var(--text-muted)', lineHeight: '1.5' }}>
            Please open your UPI app on your phone, check pending notifications, and approve the payment by entering your secret UPI PIN.
          </div>
        </div>
      )}

      {/* Expiry countdown indicator */}
      <div style={{
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'space-between',
        background: 'var(--surface-2)',
        border: '1px solid var(--border)',
        borderRadius: 'var(--radius)',
        padding: '10px 14px',
        fontSize: '12px',
        marginBottom: '20px',
      }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '6px', color: 'var(--text-secondary)' }}>
          <i className="ti ti-clock" style={{ color: timeLeft < 60 ? 'var(--text-danger)' : 'var(--text-muted)' }} />
          <span>Payment request expires in:</span>
        </div>
        <div style={{
          fontFamily: 'var(--font-mono)',
          fontWeight: 700,
          color: timeLeft < 60 ? 'var(--text-danger)' : 'var(--text-primary)',
        }}>
          {formatTimer(timeLeft)}
        </div>
      </div>

      {/* Simulated UPI Mobile App Interactive Notification (Sandbox / Dev Mode) */}
      <div style={{
        background: 'linear-gradient(180deg, #fafafa 0%, #f4f3f0 100%)',
        border: '1px solid #e2e0d8',
        borderRadius: '12px',
        padding: '16px',
        position: 'relative',
        boxShadow: 'inset 0 1px 3px rgba(0,0,0,0.03)',
      }}>
        <div style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          marginBottom: '10px',
        }}>
          <div style={{
            display: 'flex',
            alignItems: 'center',
            gap: '6px',
            fontSize: '11px',
            fontWeight: 700,
            textTransform: 'uppercase',
            letterSpacing: '0.5px',
            color: 'var(--fill-accent)',
          }}>
            <i className="ti ti-device-mobile" />
            UPI App Simulator (Test Sandbox)
          </div>

          <label style={{
            display: 'flex',
            alignItems: 'center',
            gap: '6px',
            fontSize: '11px',
            color: 'var(--text-secondary)',
            cursor: 'pointer',
          }}>
            <input
              type="checkbox"
              checked={autoApprove}
              onChange={(e) => setAutoApprove(e.target.checked)}
            />
            Auto-approve {autoApproveCountdown !== null ? `(${autoApproveCountdown}s)` : ''}
          </label>
        </div>

        {/* Mock Phone Notification Banner */}
        <div style={{
          background: '#ffffff',
          border: '1px solid #e1e0d9',
          borderRadius: '8px',
          padding: '12px',
          marginBottom: '12px',
          boxShadow: '0 2px 6px rgba(0,0,0,0.04)',
        }}>
          <div style={{ display: 'flex', gap: '10px', alignItems: 'flex-start' }}>
            <div style={{
              width: '32px',
              height: '32px',
              borderRadius: '8px',
              background: '#5f259f',
              color: '#fff',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              fontSize: '16px',
              flexShrink: 0,
            }}>
              ⚡
            </div>
            <div style={{ flex: 1 }}>
              <div style={{ fontSize: '12px', fontWeight: 700, color: '#1a1a19' }}>
                Payment Request: RoastCo Coffee
              </div>
              <div style={{ fontSize: '11px', color: '#6d6b67', marginTop: '2px' }}>
                RoastCo Coffee (roastco@icici) requested <strong>₹{formattedAmount}</strong> from your account.
              </div>
            </div>
          </div>
        </div>

        {/* Action buttons inside simulator */}
        <div style={{ display: 'flex', gap: '10px' }}>
          <button
            type="button"
            disabled={simulating}
            onClick={() => setShowPinModal(true)}
            style={{
              flex: 1,
              height: '38px',
              background: '#008744',
              color: '#ffffff',
              border: 'none',
              borderRadius: '6px',
              fontWeight: 600,
              fontSize: '12px',
              display: 'inline-flex',
              alignItems: 'center',
              justifyContent: 'center',
              gap: '6px',
              cursor: simulating ? 'not-allowed' : 'pointer',
            }}
          >
            {simulating ? (
              <>
                <i className="ti ti-loader-2 ti-spin" /> Verifying PIN...
              </>
            ) : (
              <>
                <i className="ti ti-shield-check" /> Approve & Enter UPI PIN
              </>
            )}
          </button>

          <button
            type="button"
            disabled={simulating}
            onClick={handleDeclinePayment}
            style={{
              height: '38px',
              padding: '0 14px',
              background: '#ffffff',
              color: 'var(--text-danger)',
              border: '1px solid #fad2d2',
              borderRadius: '6px',
              fontWeight: 600,
              fontSize: '12px',
              cursor: simulating ? 'not-allowed' : 'pointer',
            }}
          >
            Decline
          </button>
        </div>
      </div>

      {/* Simulated UPI 4-digit / 6-digit PIN Modal */}
      {showPinModal && (
        <div style={{
          position: 'fixed',
          top: 0,
          left: 0,
          right: 0,
          bottom: 0,
          background: 'rgba(0, 0, 0, 0.65)',
          backdropFilter: 'blur(3px)',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          zIndex: 1000,
        }}>
          <div style={{
            background: '#ffffff',
            borderRadius: '16px',
            padding: '28px',
            width: '100%',
            maxWidth: '360px',
            boxShadow: '0 20px 40px rgba(0,0,0,0.2)',
            textAlign: 'center',
          }}>
            <div style={{
              width: '44px',
              height: '44px',
              borderRadius: '50%',
              background: '#f0fbf4',
              color: '#008744',
              fontSize: '22px',
              display: 'inline-flex',
              alignItems: 'center',
              justifyContent: 'center',
              marginBottom: '12px',
            }}>
              🔒
            </div>

            <div style={{ fontSize: '16px', fontWeight: 700, marginBottom: '4px', color: '#111' }}>
              Enter 4-Digit UPI PIN
            </div>
            <div style={{ fontSize: '12px', color: '#666', marginBottom: '20px' }}>
              Authorizing payment of <strong>₹{formattedAmount}</strong> to RoastCo
            </div>

            {pinError && (
              <div style={{ fontSize: '12px', color: 'var(--text-danger)', marginBottom: '14px', fontWeight: 600 }}>
                Please enter all 4 digits
              </div>
            )}

            {/* PIN Dots */}
            <div style={{
              display: 'flex',
              justifyContent: 'center',
              gap: '12px',
              marginBottom: '20px',
            }}>
              {[0, 1, 2, 3].map((idx) => (
                <div
                  key={idx}
                  style={{
                    width: '16px',
                    height: '16px',
                    borderRadius: '50%',
                    border: '2px solid #333',
                    background: pinInput.length > idx ? '#333' : 'transparent',
                    transition: 'all 0.15s ease',
                  }}
                />
              ))}
            </div>

            {/* Numeric Keypad */}
            <div style={{
              display: 'grid',
              gridTemplateColumns: 'repeat(3, 1fr)',
              gap: '10px',
              maxWidth: '260px',
              margin: '0 auto 20px',
            }}>
              {[1, 2, 3, 4, 5, 6, 7, 8, 9, 'C', 0, '✓'].map((key) => (
                <button
                  key={String(key)}
                  type="button"
                  onClick={() => {
                    if (key === 'C') {
                      setPinInput((prev) => prev.slice(0, -1));
                    } else if (key === '✓') {
                      if (pinInput.length === 4) {
                        handleApprovePayment();
                      } else {
                        setPinError(true);
                      }
                    } else {
                      if (pinInput.length < 4) {
                        const next = pinInput + key;
                        setPinInput(next);
                        if (next.length === 4) {
                          setTimeout(() => handleApprovePayment(), 200);
                        }
                      }
                    }
                  }}
                  style={{
                    height: '46px',
                    borderRadius: '8px',
                    border: '1px solid #e1e0d9',
                    background: key === '✓' ? '#008744' : '#f9f9f7',
                    color: key === '✓' ? '#ffffff' : '#1e1c1b',
                    fontSize: '16px',
                    fontWeight: 700,
                    cursor: 'pointer',
                  }}
                >
                  {key}
                </button>
              ))}
            </div>

            <button
              type="button"
              onClick={() => {
                setShowPinModal(false);
                setPinInput('');
              }}
              style={{
                background: 'none',
                border: 'none',
                color: '#666',
                fontSize: '12px',
                cursor: 'pointer',
                textDecoration: 'underline',
              }}
            >
              Cancel
            </button>
          </div>
        </div>
      )}
    </div>
  );
};
