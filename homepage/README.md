# Msg2Git Homepage

Static website for Msg2Git service hosted at www.msg2git.com

## Structure

```
homepage/
├── index.html          # Main landing page
├── privacy.html        # Privacy Policy
├── terms.html          # Terms of Service  
├── refund.html         # Refund Policy
├── payment-success.html # Payment success page (auto-closes in 5s)
├── Dockerfile          # Minimal nginx container
├── docker-compose.yml  # Docker compose with Traefik labels
└── README.md          # This file
```

## Features

- **Responsive Design**: Mobile-first responsive layout
- **Clean URLs**: `/privacy`, `/terms`, `/refund`, `/payment-success`
- **Auto-close**: Payment success page closes automatically after 5 seconds
- **Security Headers**: XSS protection, content type sniffing protection
- **Gzip Compression**: Enabled for better performance
- **Health Checks**: Built-in health endpoint at `/health`
- **SSL Ready**: Traefik labels for automatic HTTPS with Let's Encrypt

## Local Development

```bash
# Build and run locally
docker-compose up --build

# Or run with nginx directly
nginx -p $(pwd) -c nginx.conf
```

## Production Deployment

1. **Domain Setup**: Point `www.msg2git.com` to your server
2. **Traefik**: Ensure Traefik is running with Let's Encrypt resolver
3. **Deploy**: 
   ```bash
   docker-compose up -d
   ```

## Environment Variables

The container accepts these environment variables:

- `NGINX_HOST`: Hostname (default: localhost)
- `NGINX_PORT`: Port (default: 80)

## Legal Pages

All legal pages are comprehensive and include:

- **Privacy Policy**: GDPR-compliant, covers GitHub integration, Telegram bot, Stripe payments
- **Terms of Service**: Covers acceptable use, intellectual property, subscription terms
- **Refund Policy**: 30-day money-back guarantee with clear process

## Payment Success Page

Special features:
- Auto-closes in 5 seconds
- Fallback redirect to Telegram bot if close fails
- Manual close option
- Keyboard shortcut (Escape key)
- Progress indicator
- Mobile-friendly

## Security

- Runs as non-root user (`nginx`)
- Security headers included
- Health checks enabled
- Minimal attack surface (static files only)

## Performance

- Gzip compression enabled
- Static asset caching (1 year)
- Lightweight Alpine Linux base
- Optimized nginx configuration

## Monitoring

Health check endpoint available at `/health` returns:
- Status: 200 OK
- Body: "healthy"
- Content-Type: text/plain

Perfect for load balancer health checks and monitoring systems.