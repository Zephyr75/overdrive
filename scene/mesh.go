package scene

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

)

type MeshXml struct {
  Obj string `xml:"obj"`
  Mtl string `xml:"mtl"`
}

type Mesh struct {
  Vertices [][]float32
  Faces [][]uint32
}

func (m MeshXml) ToMesh() Mesh {
  var mesh Mesh
  mesh = mesh.loadData(m.Obj, m.Mtl)
  return mesh
}


func (m Mesh) loadData(obj, mtl string) Mesh {
  objFile, err := os.Open("assets/meshes/" + obj)
  if err != nil {
    fmt.Println("Error opening file:", err)
    return m
  }
  defer objFile.Close()

  var positions []float32
  var normals []float32
  var textures []float32
  var vertices []float32
  var faces [][]uint32
  var face []uint32

  objScanner := bufio.NewScanner(objFile) 
  for objScanner.Scan() {
    line := objScanner.Text()
    split_line := strings.Split(line[2:], " ")
    // remove leading space
    if split_line[0] == "" {
      split_line = split_line[1:]
    }
    switch line[0] {
    case 'v':
      first, _ := strconv.ParseFloat(split_line[0], 32)
      second, _ := strconv.ParseFloat(split_line[1], 32)
      switch line[1] {
      case ' ': 
        third, _ := strconv.ParseFloat(split_line[2], 32)
        positions = append(positions, float32(first))
        positions = append(positions, float32(second))
        positions = append(positions, float32(third))
      case 't':
        textures = append(textures, float32(first))
        textures = append(textures, float32(second))
      case 'n':
        third, _ := strconv.ParseFloat(split_line[2], 32)
        normals = append(normals, float32(first))
        normals = append(normals, float32(second))
        normals = append(normals, float32(third))
      }
    case 'u':
      faces = append(faces, face)
      face = nil
    case 'f':
      for i := 0; i < 3; i++ {
        split_face := strings.Split(split_line[i], "/")
        first, _ := strconv.ParseUint(split_face[0], 10, 32)
        second, _ := strconv.ParseUint(split_face[1], 10, 32)
        third, _ := strconv.ParseUint(split_face[2], 10, 32)
        face = append(face, uint32(first))
        face = append(face, uint32(second))
        face = append(face, uint32(third))
      }
    }
  }
  faces = append(faces, face)

  for i := 0; i < len(faces); i++ {
    for j := 0; j < len(faces[i]); j+=3 {
      posIndex := faces[i][j] - 1
      texIndex := faces[i][j+1] - 1
      normIndex := faces[i][j+2] - 1
      vertices = append(vertices, positions[posIndex*3])
      vertices = append(vertices, positions[posIndex*3+1])
      vertices = append(vertices, positions[posIndex*3+2])
      vertices = append(vertices, normals[normIndex*3])
      vertices = append(vertices, normals[normIndex*3+1])
      vertices = append(vertices, normals[normIndex*3+2])
      vertices = append(vertices, textures[texIndex*2])
      vertices = append(vertices, textures[texIndex*2+1])
    }
  }

  m.Vertices = append(m.Vertices, vertices)
  m.Faces = faces

  return m
}
