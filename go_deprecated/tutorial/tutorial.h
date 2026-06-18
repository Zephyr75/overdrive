#pragma once
#include <string>
#include <vector>

// --- Namespace ---
namespace tut {

// --- Enum class (scoped, typed) ---
enum class Color { Red, Green, Blue };

// --- Struct (public by default) ---
struct Point {
    float x{}, y{};
    float length() const;
};

// --- Class with RAII, rule of five ---
class Buffer {
public:
    explicit Buffer(size_t size);
    ~Buffer();
    Buffer(const Buffer&);            // copy ctor
    Buffer& operator=(const Buffer&); // copy assign
    Buffer(Buffer&&) noexcept;        // move ctor
    Buffer& operator=(Buffer&&) noexcept;

    size_t size() const { return size_; }
    int& operator[](size_t i) { return data_[i]; }

private:
    int*   data_{nullptr};
    size_t size_{0};
};

// --- Template function ---
template<typename T>
T clamp(T val, T lo, T hi) { return val < lo ? lo : val > hi ? hi : val; }

// --- Variadic template ---
template<typename... Args>
void log(Args&&... args);

} // namespace tut
