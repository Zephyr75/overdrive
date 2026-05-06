// tutorial.cpp — dense C++ feature tour
#include "tutorial.h"

#include <algorithm>
#include <array>
#include <iostream>
#include <memory>
#include <optional>
#include <string>
#include <variant>
#include <vector>

// ── Namespace impl ────────────────────────────────────────────────────────────
namespace tut {

float Point::length() const {
    return std::sqrt(x * x + y * y);
}

// Buffer (RAII / rule of five)
Buffer::Buffer(size_t size) : data_{new int[size]{}}, size_{size} {}
Buffer::~Buffer() { delete[] data_; }
Buffer::Buffer(const Buffer& o) : data_{new int[o.size_]{}}, size_{o.size_} {
    std::copy(o.data_, o.data_ + size_, data_);
}
Buffer& Buffer::operator=(const Buffer& o) {
    if (this != &o) { Buffer tmp{o}; *this = std::move(tmp); } return *this;
}
Buffer::Buffer(Buffer&& o) noexcept : data_{o.data_}, size_{o.size_} {
    o.data_ = nullptr; o.size_ = 0;
}
Buffer& Buffer::operator=(Buffer&& o) noexcept {
    if (this != &o) { delete[] data_; data_ = o.data_; size_ = o.size_;
                      o.data_ = nullptr; o.size_ = 0; } return *this;
}

// Variadic template — fold expression (C++17)
template<typename... Args>
void log(Args&&... args) {
    ((std::cout << std::forward<Args>(args) << ' '), ...);
    std::cout << '\n';
}

} // namespace tut

// ── Helpers ───────────────────────────────────────────────────────────────────

// constexpr — compile-time evaluation
constexpr int factorial(int n) { return n <= 1 ? 1 : n * factorial(n - 1); }

// std::optional — nullable return without pointers
std::optional<int> parse(const std::string& s) {
    if (s.empty()) return std::nullopt;
    return std::stoi(s);
}

// ── Main ──────────────────────────────────────────────────────────────────────
int main() {

    // Uniform initialisation (prefer {} over =)
    int           a{42};
    double        pi{3.14159};
    std::string   hello{"world"};
    std::vector   nums{1, 2, 3, 4, 5}; // CTAD (C++17)

    // auto + structured bindings (C++17)
    auto [x, y] = std::pair{1.0f, 2.0f};
    std::cout << "pair: " << x << ", " << y << '\n';

    // Range-based for + const ref
    for (const auto& n : nums) std::cout << n << ' ';
    std::cout << '\n';

    // Lambda (capture, mutable, trailing return)
    auto square = [](int v) -> int { return v * v; };
    std::transform(nums.begin(), nums.end(), nums.begin(), square);

    // if-initialiser (C++17)
    if (auto val = parse("7"); val) std::cout << "parsed: " << *val << '\n';

    // constexpr
    constexpr int fact5{factorial(5)};
    static_assert(fact5 == 120);

    // Scoped enum
    tut::Color c{tut::Color::Green};
    switch (c) {
        case tut::Color::Red:   std::cout << "red\n";   break;
        case tut::Color::Green: std::cout << "green\n"; break;
        case tut::Color::Blue:  std::cout << "blue\n";  break;
    }

    // Smart pointers (RAII, no manual delete)
    auto buf = std::make_unique<tut::Buffer>(8);
    (*buf)[0] = 99;
    std::cout << "buf[0]=" << (*buf)[0] << " size=" << buf->size() << '\n';

    auto shared = std::make_shared<tut::Point>(tut::Point{3.f, 4.f});
    std::cout << "length=" << shared->length() << '\n';

    // std::variant (type-safe union, C++17)
    std::variant<int, std::string> v{"hello"};
    std::visit([](auto&& val) { std::cout << val << '\n'; }, v);
    v = 42;
    std::cout << std::get<int>(v) << '\n';

    // std::array (fixed size, stack)
    std::array<int, 4> arr{10, 20, 30, 40};
    auto it = std::find(arr.begin(), arr.end(), 30);
    if (it != arr.end()) std::cout << "found: " << *it << '\n';

    // Template + clamp
    std::cout << tut::clamp(150, 0, 100) << '\n';

    // Variadic log
    tut::log("values:", a, pi, hello);

    // Move semantics demo
    tut::Buffer b1{4};
    tut::Buffer b2{std::move(b1)}; // b1 is now empty
    std::cout << "moved size=" << b2.size() << '\n';

    return 0;
}
