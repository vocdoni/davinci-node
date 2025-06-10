import { Box, Container, Flex, Heading, HStack, Text } from '@chakra-ui/react'
import { FaDatabase } from 'react-icons/fa'

export const Navbar = () => (
  <Box bg="white" shadow="sm" borderBottom="1px" borderColor="gray.200">
    <Container maxW="container.xl">
      <Flex h={16} alignItems="center" justifyContent="space-between">
        <HStack spacing={3}>
          <Box color="purple.500">
            <FaDatabase size={24} />
          </Box>
          <Heading size="md" color="gray.700">
            Davinci Sequencer Dashboard
          </Heading>
        </HStack>
        <Text fontSize="sm" color="gray.500">
          Last refresh: {new Date().toLocaleTimeString()}
        </Text>
      </Flex>
    </Container>
  </Box>
)
