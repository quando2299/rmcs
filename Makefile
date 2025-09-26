# Makefile for C++ RMCS example

CXX = g++
CXXFLAGS = -std=c++11 -Wall
LDFLAGS = -L. -lrmcs -Wl,-rpath,.

# Target executable
TARGET = rmcs_example

# Source files
SOURCES = example_usage.cpp

# Object files
OBJECTS = $(SOURCES:.cpp=.o)

# Build target
all: $(TARGET)

$(TARGET): $(OBJECTS)
	$(CXX) $(OBJECTS) -o $(TARGET) $(LDFLAGS)

# Compile source files
%.o: %.cpp
	$(CXX) $(CXXFLAGS) -c $< -o $@

# Clean build artifacts
clean:
	rm -f $(OBJECTS) $(TARGET)

# Run the example
run: $(TARGET)
	./$(TARGET)

.PHONY: all clean run