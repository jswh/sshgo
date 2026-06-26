FROM caddy:alpine

RUN apk add --no-cache openssh-server sudo netcat-openbsd

# Set root and testuser passwords
RUN echo 'root:rootpass' | chpasswd && \
    adduser -D testuser && \
    echo 'testuser:testpass' | chpasswd

# Configure sudo: specific commands NOPASSWD for exec --sudo testing
RUN echo 'testuser ALL=(ALL) NOPASSWD: /bin/echo, /usr/bin/whoami, /bin/ls, /usr/bin/cat, /usr/bin/id' >> /etc/sudoers

# Configure SSH
RUN sed -i 's/^#PermitRootLogin yes/PermitRootLogin yes/' /etc/ssh/sshd_config && \
    sed -i 's/^#PasswordAuthentication yes/PasswordAuthentication yes/' /etc/ssh/sshd_config && \
    sed -i 's/^#PubkeyAuthentication yes/PubkeyAuthentication yes/' /etc/ssh/sshd_config && \
    echo 'PermitEmptyPasswords no' >> /etc/ssh/sshd_config

# Allow root login with password for direct auth testing
RUN sed -i 's/^#PermitRootLogin prohibit-password/PermitRootLogin yes/' /etc/ssh/sshd_config 2>/dev/null || true

# Generate SSH host keys
RUN ssh-keygen -A

# Create .ssh directories
RUN mkdir -p /home/testuser/.ssh && chmod 700 /home/testuser/.ssh && chown testuser:testuser /home/testuser/.ssh && \
    mkdir -p /root/.ssh && chmod 700 /root/.ssh

EXPOSE 22
CMD ["/usr/sbin/sshd", "-D", "-e"]
