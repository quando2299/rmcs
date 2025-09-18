FROM dustynv/ros:melodic-ros-base-l4t-r32.7.1

ENV DEBIAN_FRONTEND=noninteractive
ENV ROS_DISTRO=melodic

# Fix GPG keys for ROS and Kitware repositories
RUN apt-get update || true \
    && apt-get install -y gnupg2 curl \
    && curl -s https://raw.githubusercontent.com/ros/rosdistro/master/ros.asc | apt-key add - \
    && apt-key adv --keyserver keyserver.ubuntu.com --recv-keys 42D5A192B819C5DA \
    && curl -s https://apt.kitware.com/keys/kitware-archive-latest.asc | apt-key add -

# Install system dependencies for video processing and MQTT
RUN rm -rf /etc/apt/sources.list.d/kitware.list \
    && apt-get update --allow-releaseinfo-change --allow-insecure-repositories \
    && apt-get install -y --allow-unauthenticated \
    git \
    wget \
    curl \
    build-essential \
    pkg-config \
    libgstreamer1.0-dev \
    libgstreamer-plugins-base1.0-dev \
    gstreamer1.0-plugins-good \
    gstreamer1.0-plugins-bad \
    gstreamer1.0-plugins-ugly \
    gstreamer1.0-libav \
    gstreamer1.0-tools \
    mosquitto-clients \
    && rm -rf /var/lib/apt/lists/*

# Install Go 1.21 for ARM64
ENV GO_VERSION=1.21.13
RUN wget https://go.dev/dl/go${GO_VERSION}.linux-arm64.tar.gz \
    && tar -C /usr/local -xzf go${GO_VERSION}.linux-arm64.tar.gz \
    && rm go${GO_VERSION}.linux-arm64.tar.gz

# Set Go environment variables
ENV PATH="/usr/local/go/bin:${PATH}"
ENV GOPATH="/go"
ENV PATH="${GOPATH}/bin:${PATH}"

# Set up workspace
WORKDIR /workspace

# Copy Go module files first for better caching
COPY go.mod go.sum ./

# Download Go dependencies
RUN go mod download

# Copy the rest of the source code
COPY . .

# Build the Go application
RUN go build -o rmcs .

# Source ROS setup
RUN echo "source /opt/ros/melodic/setup.bash" >> ~/.bashrc

# Default command to run the application
CMD ["./rmcs"]