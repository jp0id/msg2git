FROM nginx:alpine

# Copy all static files to nginx html directory
COPY . /usr/share/nginx/html/

# Remove non-static files that were copied
RUN rm -f /usr/share/nginx/html/Dockerfile \
          /usr/share/nginx/html/docker-compose.yml \
          /usr/share/nginx/html/README.md \
          /usr/share/nginx/html/nginx.conf

# Create simple nginx configuration
COPY nginx.conf /etc/nginx/conf.d/default.conf

# Expose port 80
EXPOSE 80

# Start nginx as root (default) to avoid permission issues
CMD ["nginx", "-g", "daemon off;"]