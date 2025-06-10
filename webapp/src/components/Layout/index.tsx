import { Box, ChakraProvider, Container, Flex } from '@chakra-ui/react'
import { Outlet } from 'react-router-dom'
import { theme } from '~themes/main'
import { Navbar } from './Navbar'

export const Layout = () => (
  <ChakraProvider theme={theme}>
    <Box minH="100vh" bg="gray.50">
      <Navbar />
      <Container maxW="container.xl" py={8}>
        <Outlet />
      </Container>
    </Box>
  </ChakraProvider>
)
