#include "core/App.hpp"

int main(int argc, char **argv) {
  const char *scene = argc > 1 ? argv[1] : "assets/showcase.xml";
  App app("Overdrive", 1920, 1080);
  app.run(scene);
  return 0;
}
