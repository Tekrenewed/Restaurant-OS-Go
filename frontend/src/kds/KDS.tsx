import { useEffect, useState } from 'react';
import { useParams } from 'react-router-dom';
import { ChefHat, Clock, CheckCircle2 } from 'lucide-react';

interface OrderItem {
    id: string;
    name: string;
    price_paid: number;
}

interface Order {
    id: string;
    source: string;
    items: OrderItem[];
    created_at: string;
}

export default function KDS() {
    const { storeId } = useParams();
    const [orders, setOrders] = useState<Order[]>([]);
    const [isConnected, setIsConnected] = useState(false);

    useEffect(() => {
        // Generate some dummy orders immediately so the UI looks great even without backend connection
        const dummyData: Order[] = [
            {
                id: 'UBER12X-998',
                source: 'UberEats',
                created_at: new Date(Date.now() - 600000).toISOString(),
                items: [{ id: '1', name: 'Chicken Tikka Masala', price_paid: 12.99 }, { id: '2', name: 'Garlic Naan', price_paid: 3.50 }]
            },
            {
                id: 'POS-001A',
                source: 'POS',
                created_at: new Date(Date.now() - 120000).toISOString(),
                items: [{ id: '3', name: 'Mango Falooda', price_paid: 6.99 }]
            },
            {
                id: 'DEL-X9K',
                source: 'Deliveroo',
                created_at: new Date().toISOString(),
                items: [{ id: '4', name: 'Peri Peri Half Chicken', price_paid: 8.99 }, { id: '5', name: 'Loaded Fries', price_paid: 4.99 }]
            }
        ];
        setOrders(dummyData);

        // Connect to WebSocket using the storeId filter
        // 00000000-0000-0000-0000-000000000000 is used just to safely parse if dummy
        const wsUrl = `ws://localhost:8080/ws?storeId=00000000-0000-0000-0000-000000000000`;
        const socket = new WebSocket(wsUrl);

        socket.onopen = () => setIsConnected(true);
        socket.onclose = () => setIsConnected(false);

        socket.onmessage = (event) => {
            try {
                const newOrder: Order = JSON.parse(event.data);
                setOrders(prev => [...prev, newOrder]);
                // Play notification sound
                new Audio('https://assets.mixkit.co/active_storage/sfx/2869/2869-preview.mp3').play().catch(() => { });
            } catch (e) {
                console.error("Failed to parse websocket message", e);
            }
        };

        return () => socket.close();
    }, [storeId]);

    const markDone = (id: string) => {
        // Animate out by filtering
        setOrders(prev => prev.filter(o => o.id !== id));
    };

    const getTimeElapsed = (iso: string) => {
        const min = Math.floor((Date.now() - new Date(iso).getTime()) / 60000);
        return min < 1 ? 'Just now' : `${min} min ago`;
    };

    return (
        <div className="kds-container">
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                <div style={{ display: 'flex', gap: 16, alignItems: 'center' }}>
                    <ChefHat size={32} color="var(--accent-hover)" />
                    <h1 style={{ fontSize: '2rem', fontWeight: 700 }}>Kitchen Display System</h1>
                </div>
                <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
                    <div style={{
                        width: 12, height: 12, borderRadius: '50%',
                        background: isConnected ? 'var(--success)' : 'var(--danger)',
                        boxShadow: `0 0 12px ${isConnected ? 'var(--success)' : 'var(--danger)'}`
                    }} />
                    <span style={{ fontWeight: 600, color: 'var(--text-secondary)' }}>
                        {isConnected ? 'Connected to Server' : 'Offline Mode (Demo Data)'}
                    </span>
                </div>
            </div>

            <div className="kds-grid">
                {orders.map(order => {
                    const isUrgent = Math.floor((Date.now() - new Date(order.created_at).getTime()) / 60000) > 8;

                    return (
                        <div key={order.id} className={`ticket ${order.source.toLowerCase()} ${isUrgent ? 'urgent' : ''}`}>
                            <div className="ticket-header">
                                <div>
                                    <div className="ticket-id">#{order.id.slice(0, 8)}</div>
                                    <div className="ticket-time" style={{ display: 'flex', alignItems: 'center', gap: 6, marginTop: 4 }}>
                                        <Clock size={14} />
                                        {getTimeElapsed(order.created_at)}
                                    </div>
                                </div>
                                <div className={`badge`} style={{
                                    background: 'rgba(255,255,255,0.1)',
                                    alignSelf: 'flex-start'
                                }}>
                                    {order.source}
                                </div>
                            </div>

                            <div className="ticket-items">
                                {order.items.map((item, idx) => (
                                    <div key={idx} className="ticket-item">
                                        <span className="ticket-item-qty">1x</span>
                                        <span>{item.name}</span>
                                    </div>
                                ))}
                            </div>

                            <button className="mark-done-btn" onClick={() => markDone(order.id)}>
                                <CheckCircle2 size={24} style={{ margin: '0 auto' }} />
                            </button>
                        </div>
                    );
                })}

                {orders.length === 0 && (
                    <div style={{
                        gridColumn: '1 / -1', height: '400px', display: 'flex',
                        alignItems: 'center', justifyContent: 'center',
                        color: 'var(--text-secondary)', fontSize: '1.5rem'
                    }}>
                        No active orders. Kitchen is caught up! 🎉
                    </div>
                )}
            </div>
        </div>
    );
}
