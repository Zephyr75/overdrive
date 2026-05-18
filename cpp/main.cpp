#include "core/App.hpp"

int main() {
  App app("Overdrive", 1920, 1080);
  app.run("assets/sphere.xml");
  return 0;
}
